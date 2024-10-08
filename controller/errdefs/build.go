package errdefs

import (
	"io"

	"github.com/containerd/typeurl/v2"
	"github.com/docker/buildx/util/desktop"
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

func (e *BuildError) PrintBuildDetails(w io.Writer) error {
	if e.BuildRef == "" {
		return nil
	}
	ebr := &desktop.ErrorWithBuildRef{
		Ref: e.BuildRef,
		Err: e.error,
	}
	return ebr.Print(w)
}

func WrapBuild(err error, ref string, buildRef string) error {
	if err == nil {
		return nil
	}
	return &BuildError{Build: &Build{Ref: ref, BuildRef: buildRef}, error: err}
}

func (b *Build) WrapError(err error) error {
	return &BuildError{error: err, Build: b}
}
