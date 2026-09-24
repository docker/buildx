package cert

import (
	"fmt"
	"sync/atomic"
)

const (
	defaultMaxChainLength     = 10
	defaultMaxCertificateSize = 256 * 1024
)

var maxChainLength atomic.Int64
var maxCertificateSize atomic.Int64

func init() {
	maxChainLength.Store(defaultMaxChainLength)
	maxCertificateSize.Store(defaultMaxCertificateSize)
}

// Settings configures process-global validation limits for `cert.Parse()` and
// `cert.Chain` ingestion.
//
// These settings are read atomically, so changing them at runtime is race-free.
// However, concurrent parses may observe a mix of old and new values. Configure
// them once at program startup when possible.
func Settings(options ...GlobalOption) {
	for _, opt := range options {
		switch opt.Ident() {
		case identMaxChainLength{}:
			var v int
			if err := opt.Value(&v); err != nil {
				panic(fmt.Sprintf("cert.Settings: value for option WithMaxChainLength must be an int: %s", err))
			}
			if v < 0 {
				panic("cert.Settings: WithMaxChainLength must be greater than or equal to zero")
			}
			maxChainLength.Store(int64(v))
		case identMaxCertificateSize{}:
			var v int64
			if err := opt.Value(&v); err != nil {
				panic(fmt.Sprintf("cert.Settings: value for option WithMaxCertificateSize must be an int64: %s", err))
			}
			if v < 0 {
				panic("cert.Settings: WithMaxCertificateSize must be greater than or equal to zero")
			}
			maxCertificateSize.Store(v)
		}
	}
}

func currentMaxChainLength() int64 {
	return maxChainLength.Load()
}

func currentMaxCertificateSize() int64 {
	return maxCertificateSize.Load()
}

func validateChainLength(n int) error {
	limit := currentMaxChainLength()
	if limit == 0 || int64(n) <= limit {
		return nil
	}

	return fmt.Errorf(`certificate chain length %d exceeds maximum allowed length of %d`, n, limit)
}

func validateCertificateSize(n int) error {
	limit := currentMaxCertificateSize()
	if limit == 0 || int64(n) <= limit {
		return nil
	}

	return fmt.Errorf(`certificate size %d exceeds maximum allowed size of %d bytes`, n, limit)
}
