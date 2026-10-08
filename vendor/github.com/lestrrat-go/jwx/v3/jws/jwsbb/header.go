package jwsbb

import (
	"fmt"

	"github.com/lestrrat-go/jwx/v3/internal/base64"
	"github.com/valyala/fastjson"
)

type headerNotFoundError struct {
	key string
}

func (e headerNotFoundError) Error() string {
	return fmt.Sprintf(`jwsbb: header "%s" not found`, e.key)
}

func (e headerNotFoundError) Is(target error) bool {
	switch target.(type) {
	case headerNotFoundError, *headerNotFoundError:
		// If the target is a headerNotFoundError or a pointer to it, we
		// consider it a match
		return true
	default:
		return false
	}
}

// ErrHeaderNotFound returns an error that can be passed to `errors.Is` to check if the error is
// the result of the field not being found
func ErrHeaderNotFound() error {
	return headerNotFoundError{}
}

// ErrFieldNotFound is an alias for ErrHeaderNotFound, and is deprecated. It was a misnomer.
// It will be removed in a future release.
func ErrFieldNotFound() error {
	return ErrHeaderNotFound()
}

// Header is an object that allows you to access the JWS header in a quick and
// dirty way. It does not verify anything, it does not know anything about what
// each header field means, and it does not care about the JWS specification.
// But when you need to access the JWS header for that one field that you
// need, this is the object you want to use.
//
// As of this writing, HeaderParser cannot be used from concurrent goroutines.
// You will need to create a new instance for each goroutine that needs to parse a JWS header.
// Also, in general values obtained from this object should only be used
// while the Header object is still in scope.
//
// This type is experimental and may change or be removed in the future.
type Header interface {
	// I'm hiding this behind an interface so that users won't accidentally
	// rely on the underlying json handler implementation, nor the concrete
	// type name that jwsbb provides, as we may choose a different one in the future.
	jwsbbHeader()
}

type header struct {
	v   *fastjson.Value
	err error
}

func (h *header) jwsbbHeader() {}

// HeaderParseCompact parses a JWS header from a compact serialization format.
// You will need to call HeaderGet* functions to extract the values from the header.
//
// This function is experimental and may change or be removed in the future.
func HeaderParseCompact(buf []byte) Header {
	decoded, err := base64.Decode(buf)
	if err != nil {
		return &header{err: err}
	}
	return HeaderParse(decoded)
}

// HeaderParse parses a JWS header from a byte slice containing the decoded JSON.
// You will need to call HeaderGet* functions to extract the values from the header.
//
// Unlike HeaderParseCompact, this function does not perform any base64 decoding.
// This function is experimental and may change or be removed in the future.
func HeaderParse(decoded []byte) Header {
	var p fastjson.Parser
	v, err := p.ParseBytes(decoded)
	if err != nil {
		return &header{err: err}
	}
	return &header{
		v: v,
	}
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

// HeaderGetString returns the string value for the given key from the JWS header.
// An error is returned if the JSON was not valid, if the key does not exist,
// or if the value is not a string.
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

// HeaderGetBool returns the boolean value for the given key from the JWS header.
// An error is returned if the JSON was not valid, if the key does not exist,
// or if the value is not a boolean.
//
// This function is experimental and may change or be removed in the future.
func HeaderGetBool(h Header, key string) (bool, error) {
	v, err := headerGet(h, key)
	if err != nil {
		return false, err
	}
	return v.Bool()
}

// HeaderGetFloat64 returns the float64 value for the given key from the JWS header.
// An error is returned if the JSON was not valid, if the key does not exist,
// or if the value is not a float64.
//
// This function is experimental and may change or be removed in the future.
func HeaderGetFloat64(h Header, key string) (float64, error) {
	v, err := headerGet(h, key)
	if err != nil {
		return 0, err
	}
	return v.Float64()
}

// HeaderGetInt returns the int value for the given key from the JWS header.
// An error is returned if the JSON was not valid, if the key does not exist,
// or if the value is not an int.
//
// This function is experimental and may change or be removed in the future.
func HeaderGetInt(h Header, key string) (int, error) {
	v, err := headerGet(h, key)
	if err != nil {
		return 0, err
	}
	return v.Int()
}

// HeaderGetInt64 returns the int64 value for the given key from the JWS header.
// An error is returned if the JSON was not valid, if the key does not exist,
// or if the value is not an int64.
//
// This function is experimental and may change or be removed in the future.
func HeaderGetInt64(h Header, key string) (int64, error) {
	v, err := headerGet(h, key)
	if err != nil {
		return 0, err
	}
	return v.Int64()
}

// HeaderGetStringBytes returns the JSON string bytes for the given key
// from the JWS header, without copying. An error is returned if the JSON
// was not valid, if the key does not exist, or if the value is not a
// JSON string.
//
// WARNING: the returned slice aliases memory owned by h. It becomes
// invalid as soon as h is reused, re-parsed, or goes out of scope and is
// garbage collected. Do not retain the slice, share it across
// goroutines, or use it after any further call on h. If you need a value
// that outlives h, use [HeaderGetString], which returns a string copy.
//
// This function is experimental and may change or be removed in the future.
func HeaderGetStringBytes(h Header, key string) ([]byte, error) {
	v, err := headerGet(h, key)
	if err != nil {
		return nil, err
	}

	return v.StringBytes()
}

// HeaderHas returns true if the given key exists in the JWS header.
//
// This function is experimental and may change or be removed in the future.
func HeaderHas(h Header, key string) bool {
	_, err := headerGet(h, key)
	return err == nil
}

// HeaderGetStringArray returns a string array for the given key from the JWS header.
// An error is returned if the JSON was not valid, if the key does not exist,
// or if the value is not a JSON array of strings.
//
// This function is experimental and may change or be removed in the future.
func HeaderGetStringArray(h Header, key string) ([]string, error) {
	v, err := headerGet(h, key)
	if err != nil {
		return nil, err
	}

	arr, err := v.Array()
	if err != nil {
		return nil, err
	}

	result := make([]string, len(arr))
	for i, item := range arr {
		sb, err := item.StringBytes()
		if err != nil {
			return nil, err
		}
		result[i] = string(sb)
	}
	return result, nil
}

// HeaderGetUint returns the uint value for the given key from the JWS header.
// An error is returned if the JSON was not valid, if the key does not exist,
// or if the value is not a uint.
//
// This function is experimental and may change or be removed in the future.
func HeaderGetUint(h Header, key string) (uint, error) {
	v, err := headerGet(h, key)
	if err != nil {
		return 0, err
	}
	return v.Uint()
}

// HeaderGetUint64 returns the uint64 value for the given key from the JWS header.
// An error is returned if the JSON was not valid, if the key does not exist,
// or if the value is not a uint64.
//
// This function is experimental and may change or be removed in the future.
func HeaderGetUint64(h Header, key string) (uint64, error) {
	v, err := headerGet(h, key)
	if err != nil {
		return 0, err
	}

	return v.Uint64()
}
