package jwk

import (
	"errors"
	"fmt"
)

var cpe = &continueError{}

// ContinueError returns an opaque error that can be returned
// when a `KeyParser`, `KeyImporter`, or `KeyExporter` cannot handle the given payload,
// but would like the process to continue with the next handler.
func ContinueError() error {
	return cpe
}

type continueError struct{}

func (e *continueError) Error() string {
	return "continue parsing"
}

type importError struct {
	error
}

func (e importError) Unwrap() error {
	return e.error
}

func (importError) Is(err error) bool {
	_, ok := err.(importError)
	return ok
}

func importerr(f string, args ...any) error {
	return importError{fmt.Errorf(`jwk.Import: `+f, args...)}
}

var errDefaultImportError = importError{errors.New(`import error`)}

func ImportError() error {
	return errDefaultImportError
}

type parseError struct {
	error
}

func (e parseError) Unwrap() error {
	return e.error
}

func (parseError) Is(err error) bool {
	_, ok := err.(parseError)
	return ok
}

func bparseerr(prefix string, f string, args ...any) error {
	return parseError{fmt.Errorf(prefix+`: `+f, args...)}
}

func parseerr(f string, args ...any) error {
	return bparseerr(`jwk.Parse`, f, args...)
}

func rparseerr(f string, args ...any) error {
	return bparseerr(`jwk.ParseReader`, f, args...)
}

func sparseerr(f string, args ...any) error {
	return bparseerr(`jwk.ParseString`, f, args...)
}

func kparseerr(f string, args ...any) error {
	return bparseerr(`jwk.ParseKey`, f, args...)
}

type whitelistError struct {
	error
}

func (e whitelistError) Unwrap() error {
	return e.error
}

func (whitelistError) Is(err error) bool {
	_, ok := err.(whitelistError)
	return ok
}

var errDefaultWhitelistError = whitelistError{errors.New(`rejected by whitelist`)}

// WhitelistError returns an error that can be passed to `errors.Is` to check
// if the error is caused by a URL being rejected by a whitelist.
func WhitelistError() error {
	return errDefaultWhitelistError
}

var errDefaultParseError = parseError{errors.New(`parse error`)}

func ParseError() error {
	return errDefaultParseError
}
