package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/docker/cli/cli/config"
	"github.com/docker/cli/cli/config/types"
	"github.com/pkg/errors"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/propagation"
)

type Token struct {
	AccessToken string `json:"access_token"`
}

// login user to cloud build service and retrieve a build token.
func login(ctx context.Context, registryHostname, host, builder string) (string, error) {
	username, secret, err := getUserCreds(registryHostname)
	if err != nil {
		return "", errors.Wrap(err, "retrieving user credentials")
	}

	// Retrieve Auth token with build scope.
	scope := fmt.Sprintf("scope=builder:%s:build", builder)
	// TODO silvin: use an env var to override the auth host.
	authURL := fmt.Sprintf("%s/token?%s&%s", host, service, scope)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, authURL, nil)
	if err != nil {
		return "", errors.Wrap(err, "failed to create request")
	}
	req.SetBasicAuth(username, secret)
	resp, err := newClient(nil).Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp == nil {
			return "", errors.Wrap(err, "failed to get token")
		}
		defer resp.Body.Close()
		// Pull the first 255 chars of the response body to help with debugging.
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 255))
		return "", errors.Errorf("got %s. Trace ID: %s. Response body: %s", resp.Status,
			resp.Header.Get("x-trace-id"), string(body))
	}

	defer resp.Body.Close()
	var token Token
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return "", errors.Wrap(err, "failed to decode token")
	}
	return token.AccessToken, nil
}

// fetchHubToken retrieves a token from Hub that we can use to call Hub APIs.
func fetchHubToken(ctx context.Context, registryHostname, hubHost string) (string, error) {
	username, secret, err := getUserCreds(registryHostname)
	if err != nil {
		return "", errors.Wrap(err, "retrieving user credentials")
	}

	hubURL, err := url.Parse(hubHost)
	if err != nil {
		return "", errors.Wrap(err, "invalid Docker Hub URL")
	}
	tokenURL, err := hubURL.Parse("v2/auth/token")
	if err != nil {
		return "", errors.Wrap(err, "invalid token URL")
	}
	var payload struct {
		Identifier string `json:"identifier"`
		Secret     string `json:"secret"`
	}
	payload.Identifier = username
	payload.Secret = secret
	body, err := json.Marshal(payload)
	if err != nil {
		return "", errors.Wrap(err, "failed to marshal request body")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL.String(), bytes.NewReader(body))
	if err != nil {
		return "", errors.Wrap(err, "create request")
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := newClient(nil).Do(req)
	if err != nil {
		return "", errors.Wrap(err, "get token")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Pull the first 255 chars of the response body to help with debugging.
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 255))
		return "", errors.Errorf("failed to get token, got: %s. Trace ID: %s. Response body: %s", resp.Status,
			resp.Header.Get("x-trace-id"), string(body))
	}

	var respPayload struct {
		Token string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&respPayload); err != nil {
		return "", errors.Wrap(err, "decode token")
	}
	return respPayload.Token, nil
}

// getUserCreds retrieves user credentials from the credential store.
func getUserCreds(registryHostname string) (string, string, error) {
	dockerConfig := config.LoadDefaultConfigFile(os.Stderr)
	authConfig, err := dockerConfig.GetAuthConfig(registryHostname)
	if err != nil {
		return "", "", errors.Wrap(err, "retrieving auth config")
	}
	// If the store has no credentials, it will return a zero value and no
	// error, eventually failing with a difficult to understand error later.
	// Make it explicit here.
	if authConfig == (types.AuthConfig{}) {
		return "", "", errors.Errorf("no credentials found for %s", registryHostname)
	}
	username := authConfig.Username
	secret := authConfig.Password
	if authConfig.IdentityToken != "" {
		secret = authConfig.IdentityToken
	}

	return username, secret, nil
}

const (
	retryWaitInitial = 100 * time.Millisecond
	retryWaitMax     = 10 * time.Second
	retryMax         = 5
)

type retryClient struct {
	client *http.Client
}

func newClient(transport http.RoundTripper) *retryClient {
	if transport == nil {
		transport = http.DefaultTransport
	}
	propagators := otelhttp.WithPropagators(
		propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}),
	)
	return &retryClient{client: &http.Client{
		Transport: otelhttp.NewTransport(transport, propagators),
	}}
}

func (c *retryClient) Do(req *http.Request) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		attemptReq := req.Clone(req.Context())
		if attempt > 0 && req.Body != nil {
			if req.GetBody == nil {
				return nil, errors.New("request body cannot be replayed")
			}
			body, err := req.GetBody()
			if err != nil {
				return nil, err
			}
			attemptReq.Body = body
		}

		resp, err := c.client.Do(attemptReq)
		if !shouldRetry(req.Context(), resp, err) {
			return resp, err
		}
		if attempt == retryMax {
			if resp != nil {
				drainResponse(resp)
			}
			if err != nil {
				return nil, errors.Wrapf(err, "%s %s giving up after %d attempts", req.Method, req.URL, attempt+1)
			}
			return nil, errors.Errorf("%s %s giving up after %d attempts", req.Method, req.URL, attempt+1)
		}
		if resp != nil {
			drainResponse(resp)
		}

		wait := min(retryWaitInitial<<attempt, retryWaitMax)
		timer := time.NewTimer(wait)
		select {
		case <-req.Context().Done():
			timer.Stop()
			return nil, context.Cause(req.Context())
		case <-timer.C:
		}
	}
}

func shouldRetry(ctx context.Context, resp *http.Response, err error) bool {
	if context.Cause(ctx) != nil {
		return false
	}
	if err != nil {
		return true
	}
	return resp.StatusCode == http.StatusTooManyRequests ||
		(resp.StatusCode >= 500 && resp.StatusCode != http.StatusNotImplemented)
}

func drainResponse(resp *http.Response) {
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
}

const tokenRefreshInterval = time.Minute

type tokenSource func(context.Context) (string, error)

// nextTokenFunc refreshes the token every minute.
//
// TODO(nicks): Unpack the token and look at its expiry to determine when to refresh it.
func newTokenSource(fetch func(context.Context) (string, error), now func() time.Time) tokenSource {
	var mu sync.Mutex
	var token string
	var lastRefresh time.Time
	return tokenSource(func(ctx context.Context) (string, error) {
		mu.Lock()
		defer mu.Unlock()

		if lastRefresh.IsZero() || now().Sub(lastRefresh) > tokenRefreshInterval {
			ctx, cancel := context.WithTimeoutCause(ctx, 30*time.Second, errors.WithStack(context.DeadlineExceeded))
			defer cancel()
			next, err := fetch(ctx)
			if err != nil {
				return "", err
			}
			token = next
			lastRefresh = now()
		}
		return token, nil
	})
}
