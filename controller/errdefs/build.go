package errdefs

import (
	"github.com/containerd/typeurl/v2"
	"github.com/moby/buildkit/util/grpcerrors"
)

func init() {
	typeurl.Register((*Build)(nil), "github.com/docker/buildx", "errdefs.Build+json")
}

type BuildError struct {
	*Build
	error
}

func (e *BuildError) Unwrap() error {
	return e.error
}

func (e *BuildError) ToProto() grpcerrors.TypedErrorProto {
	return e.Build
}

func WrapBuild(err error, ref string) error {
	if err == nil {
		return nil
	}
	return &BuildError{Build: &Build{Ref: ref}, error: err}
}

func (b *Build) WrapError(err error) error {
	return &BuildError{error: err, Build: b}
}
