package cert

import "github.com/lestrrat-go/option/v2"

// GlobalOption describes an option that can be passed to `cert.Settings()`.
type GlobalOption interface {
	option.Interface
	globalOption()
}

type globalOption struct {
	option.Interface
}

func (*globalOption) globalOption() {}

type identMaxChainLength struct{}
type identMaxCertificateSize struct{}

// WithMaxChainLength specifies the maximum number of certificates allowed in
// a certificate chain handled by `cert.Chain`.
//
// The default is 10. Set to 0 to disable the limit.
func WithMaxChainLength(v int) GlobalOption {
	return &globalOption{option.New(identMaxChainLength{}, v)}
}

// WithMaxCertificateSize specifies the maximum decoded DER size, in bytes,
// accepted by `cert.Parse()` and `cert.Chain` ingestion.
//
// The default is 256 KiB. Set to 0 to disable the limit.
func WithMaxCertificateSize(v int64) GlobalOption {
	return &globalOption{option.New(identMaxCertificateSize{}, v)}
}
