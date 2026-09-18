package jwk

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// defaultMaxFetchBodySize is the initial default maximum number of bytes read
// from an HTTP response body when fetching a JWKS (10 MB).
const defaultMaxFetchBodySize int64 = 10 * 1024 * 1024

// defaultFetchTimeout is the default timeout for HTTP requests made by
// jwk.Fetch(). This prevents malicious or unresponsive JWKS endpoints from
// hanging indefinitely (e.g. slowloris-style DoS).
const defaultFetchTimeout = 30 * time.Second

// defaultMaxRedirects is the maximum number of HTTP redirects the default
// fetch client will follow. This is intentionally lower than Go's default
// of 10 to limit redirect chain abuse.
const defaultMaxRedirects = 5

var maxFetchBodySize atomic.Int64

var (
	fetchHTTPClientMu sync.RWMutex
	fetchHTTPClient   HTTPClient
)

func init() {
	maxFetchBodySize.Store(defaultMaxFetchBodySize)
	fetchHTTPClient = DefaultHTTPClient()
}

// DefaultHTTPClient returns a new http.Client configured with the same
// defaults used by jwk.Fetch(): a 30-second timeout, a redirect policy
// that blocks HTTPS-to-HTTP scheme downgrades, and a maximum of 5 redirects.
//
// This is useful for callers who need the library's default protections
// but want to wrap or augment the client (e.g. adding a custom Transport),
// and for restoring defaults after calling jwk.Configure(jwk.WithHTTPClient(...)).
func DefaultHTTPClient() *http.Client {
	return WrapHTTPClientDefaults(&http.Client{})
}

// WrapHTTPClientDefaults returns a shallow copy of the given http.Client with the
// library's default safety behaviors applied. Existing client settings
// (Transport, Jar, etc.) are preserved.
//
//   - Timeout: applied only when the client has no timeout set (zero value).
//   - CheckRedirect: if the client already has one, the library's redirect
//     policy runs first; if it passes, the original CheckRedirect is called.
//     If the client has no CheckRedirect, the library's policy is used directly.
//
// This is useful when you need to bring your own http.Client (e.g. for custom
// TLS configuration) but still want the library's redirect hardening.
func WrapHTTPClientDefaults(client *http.Client) *http.Client {
	cloned := *client
	if cloned.Timeout == 0 {
		cloned.Timeout = defaultFetchTimeout
	}
	orig := cloned.CheckRedirect
	if orig == nil {
		cloned.CheckRedirect = defaultCheckRedirect
	} else {
		cloned.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			if err := defaultCheckRedirect(req, via); err != nil {
				return err
			}
			return orig(req, via)
		}
	}
	return &cloned
}

// defaultCheckRedirect is the CheckRedirect policy for the default HTTP client
// used by jwk.Fetch(). It prevents HTTPS-to-HTTP scheme downgrades and limits
// the total number of redirects.
//
// This does NOT protect against redirects to private/internal IP addresses.
// For full SSRF protection, callers should provide a custom http.Client via
// WithHTTPClient that validates destination IPs in Transport.DialContext.
func defaultCheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= defaultMaxRedirects {
		return fmt.Errorf("jwk.Fetch: stopped after %d redirects", defaultMaxRedirects)
	}

	// Prevent HTTPS → HTTP scheme downgrade at any hop.
	// via[len(via)-1] is the immediately previous request in the chain.
	if len(via) > 0 && via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme != "https" {
		return fmt.Errorf("jwk.Fetch: redirect from HTTPS to non-HTTPS URL %q is not allowed", req.URL.Redacted())
	}
	return nil
}

func getFetchHTTPClient() HTTPClient {
	fetchHTTPClientMu.RLock()
	defer fetchHTTPClientMu.RUnlock()
	return fetchHTTPClient
}

func setFetchHTTPClient(c HTTPClient) {
	fetchHTTPClientMu.Lock()
	defer fetchHTTPClientMu.Unlock()
	fetchHTTPClient = c
}

// Fetcher is an interface that represents an object that fetches a JWKS.
// Currently this is only used in the `jws.WithVerifyAuto` option.
//
// Particularly, do not confuse this as the backend to `jwk.Fetch()` function.
// If you need to control how `jwk.Fetch()` implements HTTP requests look into
// providing a custom `http.Client` object via `jwk.WithHTTPClient` option
type Fetcher interface {
	Fetch(context.Context, string, ...FetchOption) (Set, error)
}

// FetchFunc describes a type of Fetcher that is represented as a function.
//
// You can use this to wrap functions (e.g. `jwk.Fetch“) as a Fetcher object.
type FetchFunc func(context.Context, string, ...FetchOption) (Set, error)

func (ff FetchFunc) Fetch(ctx context.Context, u string, options ...FetchOption) (Set, error) {
	return ff(ctx, u, options...)
}

// CachedFetcher wraps `jwk.Cache` so that it can be used as a `jwk.Fetcher`.
//
// One notable diffence from a general use fetcher is that `jwk.CachedFetcher`
// can only be used with JWKS URLs that have been registered with the cache.
// Please read the documentation fo `(jwk.CachedFetcher).Fetch` for more details.
//
// This object is intended to be used with `jws.WithVerifyAuto` option, specifically
// for a scenario where there is a very small number of JWKS URLs that are trusted
// and used to verify JWS messages. It is NOT meant to be used as a general purpose
// caching fetcher object.
type CachedFetcher struct {
	cache *Cache
}

// NewCachedFetcher creates a new `jwk.CachedFetcher` object.
func NewCachedFetcher(cache *Cache) *CachedFetcher {
	return &CachedFetcher{cache}
}

// Fetch fetches a JWKS from the cache. If the JWKS URL has not been registered with
// the cache, an error is returned.
func (f *CachedFetcher) Fetch(ctx context.Context, u string, _ ...FetchOption) (Set, error) {
	if !f.cache.IsRegistered(ctx, u) {
		return nil, fmt.Errorf(`jwk.CachedFetcher: url %q has not been registered`, u)
	}
	return f.cache.Lookup(ctx, u)
}

// Fetch fetches a JWK resource specified by a URL. The url must be
// pointing to a resource that is supported by `net/http`.
//
// This function is just a wrapper around `net/http` and `jwk.Parse`.
// There is nothing special here, so you are safe to use your own
// mechanism to fetch the JWKS.
//
// If you are using the same `jwk.Set` for long periods of time during
// the lifecycle of your program, and would like to periodically refresh the
// contents of the object with the data at the remote resource,
// consider using `jwk.Cache`, which automatically refreshes
// jwk.Set objects asynchronously.
//
// # Security
//
// By default, jwk.Fetch does not restrict which URLs may be contacted: the
// URL you pass is fetched as-is, with only the default HTTP client's
// HTTPS-to-HTTP redirect block applied. This is the right default when the
// URL is hard-coded or comes from configuration you control.
//
// It is NOT safe when the URL is attacker-controllable — most commonly a
// `jku` header copied out of an untrusted JWS. In that case you MUST pass
// a jwk.WithFetchWhitelist() option that restricts the reachable URLs via
// jwk.MapWhitelist, jwk.RegexpWhitelist, or a custom Whitelist.
//
// For defense against redirect-to-private-IP and DNS-rebinding attacks,
// combine WithFetchWhitelist with a custom http.Client (see WithHTTPClient)
// whose Transport.DialContext validates resolved addresses.
func Fetch(ctx context.Context, u string, options ...FetchOption) (Set, error) {
	var parseOptions []ParseOption
	//nolint:revive // I want to keep the type of `wl` as `Whitelist` instead of `InsecureWhitelist`
	var wl Whitelist = InsecureWhitelist{}
	var client = getFetchHTTPClient()
	var maxBodySize = maxFetchBodySize.Load()
	for _, option := range options {
		if parseOpt, ok := option.(ParseOption); ok {
			parseOptions = append(parseOptions, parseOpt)
			continue
		}

		switch option.Ident() {
		case identHTTPClient{}:
			if err := option.Value(&client); err != nil {
				return nil, fmt.Errorf(`failed to retrieve HTTPClient option value: %w`, err)
			}
		case identFetchWhitelist{}:
			if err := option.Value(&wl); err != nil {
				return nil, fmt.Errorf(`failed to retrieve fetch whitelist option value: %w`, err)
			}
		case identMaxFetchBodySize{}:
			if err := option.Value(&maxBodySize); err != nil {
				return nil, fmt.Errorf(`failed to retrieve MaxFetchBodySize option value: %w`, err)
			}
			if maxBodySize <= 0 {
				return nil, fmt.Errorf(`jwk.Fetch: WithMaxFetchBodySize must be greater than zero`)
			}
		}
	}

	if !wl.IsAllowed(u) {
		return nil, whitelistError{fmt.Errorf(`jwk.Fetch: url %q has been rejected by whitelist`, u)}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf(`jwk.Fetch: failed to create new request: %w`, err)
	}

	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf(`jwk.Fetch: request failed: %w`, err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(`jwk.Fetch: request returned status %d, expected 200`, res.StatusCode)
	}

	// LimitReader caps memory at maxBodySize+1; reading +1 byte lets us detect
	// oversized responses. We intentionally skip a Content-Length pre-check because
	// the header is untrustworthy (server-controlled, absent in chunked transfers).
	// Slow-trickle attacks are mitigated by context deadlines and http.Client.Timeout,
	// not by header inspection.
	buf, err := io.ReadAll(io.LimitReader(res.Body, maxBodySize+1))
	if err != nil {
		return nil, fmt.Errorf(`jwk.Fetch: failed to read response body for %q: %w`, u, err)
	}
	if int64(len(buf)) > maxBodySize {
		return nil, fmt.Errorf(`jwk.Fetch: response body for %q exceeded max size of %d bytes`, u, maxBodySize)
	}

	return Parse(buf, parseOptions...)
}
