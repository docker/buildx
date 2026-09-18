package jwa

// EdDSAEd448 returns an object representing the EdDSA signature algorithm
// using Ed448 (RFC 9864).
//
// Unlike built-in algorithms, Ed448 is not registered by default. Import
// the ed448 module for its side effects to enable Ed448 support:
//
//	import _ "github.com/lestrrat-go/jwx-circl-ed448"
//
// The function name is tentative and may change in future releases.
func EdDSAEd448() SignatureAlgorithm {
	return NewSignatureAlgorithm("Ed448")
}
