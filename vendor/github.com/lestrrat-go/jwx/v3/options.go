package jwx

import "github.com/lestrrat-go/option/v2"

type identUseNumber struct{}

type Option = option.Interface

type JSONOption interface {
	Option
	isJSONOption()
}

type jsonOption struct {
	Option
}

func (o *jsonOption) isJSONOption() {}

func newJSONOption(n any, v any) JSONOption {
	return &jsonOption{option.New(n, v)}
}

// WithUseNumber controls whether the jwx package should unmarshal
// JSON objects with the "encoding/json".Decoder.UseNumber feature on.
//
// This setting has process-global effect and must be applied once
// at program startup (typically from func init() or early in main())
// before any goroutine begins parsing JWx payloads. The underlying
// flag is read atomically, so toggling it at runtime is race-free,
// but any in-flight or subsequent decoders will observe a mix of
// float64 and json.Number values in concurrently-decoded custom
// fields — callers that type-assert on those values will break
// non-deterministically. There is no per-call override.
//
// Default is false.
func WithUseNumber(b bool) JSONOption {
	return newJSONOption(identUseNumber{}, b)
}
