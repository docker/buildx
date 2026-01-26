package auth

// based on containerd/core/remotes/docker/authorizer.go

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/containerd/containerd/v2/core/remotes/docker"
	"github.com/containerd/containerd/v2/core/remotes/docker/auth"
	remoteerrors "github.com/containerd/containerd/v2/core/remotes/errors"
	"github.com/containerd/errdefs"
	"github.com/docker/cli/cli/config/types"
	"github.com/moby/buildkit/session/auth/authprovider"
	"github.com/moby/buildkit/util/bklog"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type dockerAuthorizer struct {
	credentials authprovider.AuthConfigProvider

	client *http.Client
	header http.Header
	mu     sync.RWMutex

	// indexed by host name
	handlers map[string]*authHandler
}

type authorizerConfig struct {
	credentials authprovider.AuthConfigProvider
	client      *http.Client
	header      http.Header
}

// AuthorizerOpt configures an authorizer
type AuthorizerOpt func(*authorizerConfig)

// WithAuthClient provides the HTTP client for the authorizer
func WithAuthClient(client *http.Client) AuthorizerOpt {
	return func(opt *authorizerConfig) {
		opt.client = client
	}
}

// WithAuthCreds provides a credential function to the authorizer
func WithAuthProvider(provider authprovider.AuthConfigProvider) AuthorizerOpt {
	return func(opt *authorizerConfig) {
		opt.credentials = provider
	}
}

// WithAuthHeader provides HTTP headers for authorization
//
// We need to merge instead of replacing because header may be set by
// a per-host hosts.toml or/AND by a global header config (e.g., cri.config.headers)
func WithAuthHeader(hdr http.Header) AuthorizerOpt {
	return func(opt *authorizerConfig) {
		if opt.header == nil {
			opt.header = hdr.Clone()
		} else {
			for k, v := range hdr {
				opt.header[k] = append(opt.header[k], v...)
			}
		}
	}
}

// NewDockerAuthorizer creates an authorizer using Docker's registry
// authentication spec.
// See https://distribution.github.io/distribution/spec/auth/
func NewDockerAuthorizer(opts ...AuthorizerOpt) docker.Authorizer {
	var ao authorizerConfig
	for _, opt := range opts {
		opt(&ao)
	}

	if ao.client == nil {
		ao.client = http.DefaultClient
	}

	return &dockerAuthorizer{
		credentials: ao.credentials,
		client:      ao.client,
		header:      ao.header,
		handlers:    make(map[string]*authHandler),
	}
}

// Authorize handles auth request.
func (a *dockerAuthorizer) Authorize(ctx context.Context, req *http.Request) error {
	// skip if there is no auth handler
	ah := a.getAuthHandler(req.URL.Host)
	if ah == nil {
		return nil
	}

	auth, err := ah.authorize(ctx)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", auth)

	return nil
}

func (a *dockerAuthorizer) getAuthHandler(host string) *authHandler {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.handlers[host]
}

func (a *dockerAuthorizer) AddResponses(ctx context.Context, responses []*http.Response) error {
	last := responses[len(responses)-1]
	host := last.Request.URL.Host

	a.mu.Lock()
	defer a.mu.Unlock()
	for _, c := range auth.ParseAuthHeader(last.Header) {
		if c.Scheme == auth.BearerAuth {
			if retry, err := invalidAuthorization(ctx, c, responses); err != nil {
				delete(a.handlers, host)
				return err
			} else if retry {
				delete(a.handlers, host)
			}

			// reuse existing handler
			//
			// assume that one registry will return the common
			// challenge information, including realm and service.
			// and the resource scope is only different part
			// which can be provided by each request.
			if _, ok := a.handlers[host]; ok {
				return nil
			}

			var username, secret string
			if a.credentials != nil {
				var err error
				ac, err := a.credentials(ctx, host, strings.Split(c.Parameters["scope"], " "), nil)
				if err != nil {
					return err
				}
				username, secret = parseAuthConfig(ac)
			}

			common, err := auth.GenerateTokenOptions(ctx, host, username, secret, c)
			if err != nil {
				return err
			}

			a.handlers[host] = newAuthHandler(a.client, a.header, c.Scheme, common)
			return nil
		} else if c.Scheme == auth.BasicAuth && a.credentials != nil {
			ac, err := a.credentials(ctx, host, nil, nil)
			if err != nil {
				return err
			}

			username, secret := parseAuthConfig(ac)

			if username == "" || secret == "" {
				return errors.Wrap(err, "no basic auth credentials")
			}

			a.handlers[host] = newAuthHandler(a.client, a.header, c.Scheme, auth.TokenOptions{
				Username: username,
				Secret:   secret,
			})
			return nil
		}
	}
	return errors.Wrap(errdefs.ErrNotImplemented, "failed to find supported auth scheme")
}

func parseAuthConfig(ac types.AuthConfig) (string, string) {
	if ac.IdentityToken != "" {
		return "", ac.IdentityToken
	}
	return ac.Username, ac.Password
}

// authResult is used to control limit rate.
type authResult struct {
	sync.WaitGroup
	token          string
	expirationTime *time.Time
	err            error
}

// authHandler is used to handle auth request per registry server.
type authHandler struct {
	sync.Mutex

	header http.Header

	client *http.Client

	// only support basic and bearer schemes
	scheme auth.AuthenticationScheme

	// common contains common challenge answer
	common auth.TokenOptions

	// scopedTokens caches token indexed by scopes, which used in
	// bearer auth case
	scopedTokens map[string]*authResult
}

func newAuthHandler(client *http.Client, hdr http.Header, scheme auth.AuthenticationScheme, opts auth.TokenOptions) *authHandler {
	return &authHandler{
		header:       hdr,
		client:       client,
		scheme:       scheme,
		common:       opts,
		scopedTokens: map[string]*authResult{},
	}
}

func (ah *authHandler) authorize(ctx context.Context) (string, error) {
	switch ah.scheme {
	case auth.BasicAuth:
		return ah.doBasicAuth(ctx)
	case auth.BearerAuth:
		return ah.doBearerAuth(ctx)
	default:
		return "", errors.Wrapf(errdefs.ErrNotImplemented, "failed to find supported auth scheme: %s", string(ah.scheme))
	}
}

func (ah *authHandler) doBasicAuth(_ context.Context) (string, error) {
	username, secret := ah.common.Username, ah.common.Secret

	if username == "" || secret == "" {
		return "", errors.Errorf("failed to handle basic auth because missing username or secret")
	}

	auth := base64.StdEncoding.EncodeToString([]byte(username + ":" + secret))
	return "Basic " + auth, nil
}

func (ah *authHandler) doBearerAuth(ctx context.Context) (token string, err error) {
	// copy common tokenOptions
	to := ah.common

	to.Scopes = docker.GetTokenScopes(ctx, to.Scopes)

	// Docs: https://distribution.github.io/distribution/spec/auth/scope/
	scoped := strings.Join(to.Scopes, " ")

	// Keep track of the expiration time of cached bearer tokens so they can be
	// refreshed when they expire without a server roundtrip.
	var expirationTime *time.Time

	ah.Lock()
	if r, exist := ah.scopedTokens[scoped]; exist && (r.expirationTime == nil || r.expirationTime.After(time.Now())) {
		ah.Unlock()
		r.Wait()
		return r.token, r.err
	}

	// only one fetch token job
	r := new(authResult)
	r.Add(1)
	ah.scopedTokens[scoped] = r
	ah.Unlock()

	defer func() {
		token = "Bearer " + token
		r.token, r.err, r.expirationTime = token, err, expirationTime
		r.Done()
	}()

	// fetch token for the resource scope
	if to.Secret != "" {
		defer func() {
			if err != nil {
				err = errors.Wrap(err, "failed to fetch oauth token")
			}
		}()
		// credential information is provided, use oauth POST endpoint
		// TODO: Allow setting client_id
		resp, err := auth.FetchTokenWithOAuth(ctx, ah.client, ah.header, "containerd-client", to)
		if err != nil {
			var errStatus remoteerrors.ErrUnexpectedStatus
			if errors.As(err, &errStatus) {
				// Registries without support for POST may return 404 for POST /v2/token.
				// As of September 2017, GCR is known to return 404.
				// As of February 2018, JFrog Artifactory is known to return 401.
				// As of January 2022, ACR is known to return 400.
				if (errStatus.StatusCode == 405 && to.Username != "") || errStatus.StatusCode == 404 || errStatus.StatusCode == 401 || errStatus.StatusCode == 400 {
					resp, err := auth.FetchToken(ctx, ah.client, ah.header, to)
					if err != nil {
						return "", err
					}
					expirationTime = getExpirationTime(resp.ExpiresInSeconds)
					return resp.Token, nil
				}
				bklog.G(ctx).WithFields(logrus.Fields{
					"status": errStatus.Status,
					"body":   string(errStatus.Body),
				}).Debugf("token request failed")
			}
			return "", err
		}
		expirationTime = getExpirationTime(resp.ExpiresInSeconds)
		return resp.AccessToken, nil
	}
	// do request anonymously
	resp, err := auth.FetchToken(ctx, ah.client, ah.header, to)
	if err != nil {
		return "", errors.Wrap(err, "failed to fetch anonymous token")
	}
	expirationTime = getExpirationTime(resp.ExpiresInSeconds)
	return resp.Token, nil
}

func getExpirationTime(expiresInSeconds int) *time.Time {
	if expiresInSeconds <= 0 {
		return nil
	}
	expirationTime := time.Now().Add(time.Duration(expiresInSeconds) * time.Second)
	return &expirationTime
}

func invalidAuthorization(ctx context.Context, c auth.Challenge, responses []*http.Response) (retry bool, _ error) {
	errStr := c.Parameters["error"]
	if errStr == "" {
		return retry, nil
	}

	n := len(responses)
	if n == 1 || (n > 1 && !sameRequest(responses[n-2].Request, responses[n-1].Request)) {
		limitedErr := errStr
		errLenghLimit := 64
		if len(limitedErr) > errLenghLimit {
			limitedErr = limitedErr[:errLenghLimit] + "..."
		}
		bklog.G(ctx).WithField("error", limitedErr).Debug("authorization error using bearer token, retrying")
		return true, nil
	}

	return retry, errors.Wrapf(docker.ErrInvalidAuthorization, "server message: %s", errStr)
}

func sameRequest(r1, r2 *http.Request) bool {
	if r1.Method != r2.Method {
		return false
	}
	if *r1.URL != *r2.URL {
		return false
	}
	return true
}
