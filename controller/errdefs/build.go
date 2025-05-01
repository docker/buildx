package errdefs

type BuildError struct {
	err error
}

func (e *BuildError) Unwrap() error {
	return e.err
}

func (e *BuildError) Error() string {
	return e.err.Error()
}

func WrapBuild(err error) error {
	if err == nil {
		return nil
	}
	return &BuildError{err: err}
}
