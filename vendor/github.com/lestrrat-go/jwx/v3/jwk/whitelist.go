package jwk

import "github.com/lestrrat-go/httprc/v3"

type Whitelist = httprc.Whitelist
type WhitelistFunc = httprc.WhitelistFunc

// InsecureWhitelist is a Whitelist implementation (aliased to
// httprc.InsecureWhitelist) that allows every URL jwk.Fetch() is asked to
// retrieve. It is the library's default, which keeps first-time usage
// simple: callers with a hard-coded JWKS URL do not have to configure
// anything.
//
// Do NOT use InsecureWhitelist in any code path where the URL originates
// from untrusted input (for example, the `jku` header of a JWS). For
// those paths, construct a MapWhitelist, RegexpWhitelist, or custom
// Whitelist and pass it via jwk.WithFetchWhitelist().
type InsecureWhitelist = httprc.InsecureWhitelist

func NewInsecureWhitelist() InsecureWhitelist {
	return httprc.NewInsecureWhitelist()
}

// BlockAllWhitelist is an alias to httprc.BlockAllWhitelist. Use
// functions in the `httprc` package to interact with this type.
type BlockAllWhitelist = httprc.BlockAllWhitelist

func NewBlockAllWhitelist() BlockAllWhitelist {
	return httprc.NewBlockAllWhitelist()
}

// RegexpWhitelist is an alias to httprc.RegexpWhitelist. Use
// functions in the `httprc` package to interact with this type.
type RegexpWhitelist = httprc.RegexpWhitelist

func NewRegexpWhitelist() *RegexpWhitelist {
	return httprc.NewRegexpWhitelist()
}

// MapWhitelist is an alias to httprc.MapWhitelist. Use
// functions in the `httprc` package to interact with this type.
type MapWhitelist = httprc.MapWhitelist

func NewMapWhitelist() MapWhitelist {
	return httprc.NewMapWhitelist()
}
