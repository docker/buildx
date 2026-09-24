// Package registry provides an internal registry of JWK key constructors.
// It exists so that both jwk and jwk/jwkunsafe can access the same set of
// constructors without exporting them from the jwk package.
package registry

import "fmt"

// Constructor holds factory functions for creating empty JWK keys.
// Public may be nil for key types without a public/private distinction
// (e.g. symmetric keys).
type Constructor struct {
	Public  func() any // returns jwk.Key
	Private func() any // returns jwk.Key
}

var constructors = map[string]Constructor{}

// Register adds a constructor for the given key type name.
func Register(kty string, c Constructor) {
	constructors[kty] = c
}

// NewKey creates a new empty private (or symmetric) key for the given key type.
func NewKey(kty string) (any, error) {
	c, ok := constructors[kty]
	if !ok {
		return nil, fmt.Errorf(`registry: unknown key type %q`, kty)
	}
	return c.Private(), nil
}

// NewPublicKey creates a new empty public key for the given key type.
// Returns an error for key types that have no public/private distinction.
func NewPublicKey(kty string) (any, error) {
	c, ok := constructors[kty]
	if !ok {
		return nil, fmt.Errorf(`registry: unknown key type %q`, kty)
	}
	if c.Public == nil {
		return nil, fmt.Errorf(`registry: key type %q has no public key variant`, kty)
	}
	return c.Public(), nil
}
