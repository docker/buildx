package jwkbb

import (
	"fmt"

	"github.com/valyala/fastjson"
)

type headerNotFoundError struct {
	key string
}

func (e headerNotFoundError) Error() string {
	return fmt.Sprintf(`jwkbb: field "%s" not found`, e.key)
}

func (e headerNotFoundError) Is(target error) bool {
	switch target.(type) {
	case headerNotFoundError, *headerNotFoundError:
		return true
	default:
		return false
	}
}

// ErrHeaderNotFound returns an error that can be passed to errors.Is
// to check if the error is the result of a field not being found in
// the parsed JSON.
func ErrHeaderNotFound() error {
	return headerNotFoundError{}
}

// Header is an opaque handle to a parsed JWK or JWKS JSON object.
// It exists for fast, allocation-light field probing without paying
// the cost of a full encoding/json unmarshal.
//
// Header instances are NOT safe for concurrent use. Create a new one
// per goroutine. Values returned by HeaderGet* helpers may alias
// memory owned by the Header; do not retain them past the Header's
// lifetime unless the helper explicitly copies (HeaderGetString does;
// HeaderGetStringBytes does not).
//
// This type is experimental and may change or be removed in the future.
type Header interface {
	// Sealed so callers can't depend on the underlying fastjson type
	// or substitute their own implementation.
	jwkbbHeader()
}

type header struct {
	v   *fastjson.Value
	err error
}

func (h *header) jwkbbHeader() {}

// HeaderParse parses a JSON byte slice and returns a Header for fast
// field access. Parse errors are deferred to the first HeaderGet* /
// HeaderHas call.
//
// This function is experimental and may change or be removed in the future.
func HeaderParse(buf []byte) Header {
	var p fastjson.Parser
	v, err := p.ParseBytes(buf)
	if err != nil {
		return &header{err: err}
	}
	return &header{v: v}
}

func headerGet(h Header, key string) (*fastjson.Value, error) {
	//nolint:forcetypeassert
	hh := h.(*header) // we _know_ this can't be another type
	if hh.err != nil {
		return nil, hh.err
	}

	v := hh.v.Get(key)
	if v == nil {
		return nil, headerNotFoundError{key: key}
	}
	return v, nil
}

// HeaderHas reports whether the given key exists in the parsed JSON object.
// Returns false on parse errors.
//
// This function is experimental and may change or be removed in the future.
func HeaderHas(h Header, key string) bool {
	_, err := headerGet(h, key)
	return err == nil
}

// HeaderGetString returns the string value for the given key as a
// freshly-allocated Go string. The returned value remains valid after
// the Header is garbage collected.
//
// This function is experimental and may change or be removed in the future.
func HeaderGetString(h Header, key string) (string, error) {
	v, err := headerGet(h, key)
	if err != nil {
		return "", err
	}

	sb, err := v.StringBytes()
	if err != nil {
		return "", err
	}

	return string(sb), nil
}

// HeaderGetStringBytes returns the JSON string bytes for the given key
// without copying.
//
// WARNING: the returned slice aliases memory owned by h. It becomes
// invalid as soon as h is reused, re-parsed, or goes out of scope and
// is garbage collected. Do not retain the slice, share it across
// goroutines, or use it after any further call on h. If you need a
// value that outlives h, use [HeaderGetString].
//
// This function is experimental and may change or be removed in the future.
func HeaderGetStringBytes(h Header, key string) ([]byte, error) {
	v, err := headerGet(h, key)
	if err != nil {
		return nil, err
	}

	return v.StringBytes()
}
