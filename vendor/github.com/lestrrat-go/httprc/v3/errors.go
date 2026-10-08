package httprc

import "errors"

var errResourceAlreadyExists = errors.New(`resource already exists`)

func ErrResourceAlreadyExists() error {
	return errResourceAlreadyExists
}

var errAlreadyRunning = errors.New(`client is already running`)

func ErrAlreadyRunning() error {
	return errAlreadyRunning
}

var errResourceNotFound = errors.New(`resource not found`)

func ErrResourceNotFound() error {
	return errResourceNotFound
}

var errTransformerRequired = errors.New(`transformer is required`)

func ErrTransformerRequired() error {
	return errTransformerRequired
}

var errURLCannotBeEmpty = errors.New(`URL cannot be empty`)

func ErrURLCannotBeEmpty() error {
	return errURLCannotBeEmpty
}

var errUnexpectedStatusCode = errors.New(`unexpected status code`)

func ErrUnexpectedStatusCode() error {
	return errUnexpectedStatusCode
}

var errTransformerFailed = errors.New(`failed to transform response body`)

func ErrTransformerFailed() error {
	return errTransformerFailed
}

var errRecoveredFromPanic = errors.New(`recovered from panic`)

func ErrRecoveredFromPanic() error {
	return errRecoveredFromPanic
}

var errBlockedByWhitelist = errors.New(`blocked by whitelist`)

func ErrBlockedByWhitelist() error {
	return errBlockedByWhitelist
}

var errNotReady = errors.New(`resource registered but not ready`)

// ErrNotReady returns a sentinel error indicating that the resource was
// successfully registered with the backend and is being actively managed,
// but the first fetch and transformation has not completed successfully yet.
//
// This error is returned by Add() when:
//   - The resource was successfully added to the backend (registration succeeded)
//   - WithWaitReady(true) was specified (the default)
//   - The Ready() call failed (timeout, transform error, context cancelled, etc.)
//
// When Add() returns this error, the resource IS in the backend's resource map
// and will continue to be fetched periodically in the background according to
// the refresh interval. The application can safely proceed - the resource data
// may become available later when a fetch succeeds.
//
// IMPORTANT: "Not ready" means the first fetch and transformation has not completed
// successfully. The resource may eventually become ready (if the transformation
// succeeds on a subsequent retry), or it may never become ready (if the data is
// permanently invalid or the server is unreachable). The backend will continue
// retrying according to the configured refresh interval.
//
// The underlying error (context deadline, transform failure, etc.) is wrapped
// using Go 1.20+ multiple error wrapping and can be examined with errors.Is()
// or errors.As(). You do not need to manually unwrap the error.
//
// Example:
//
//	err := ctrl.Add(ctx, resource)
//	if err != nil {
//	    if errors.Is(err, httprc.ErrNotReady()) {
//	        // Resource registered, will fetch in background
//	        log.Print("Resource not ready yet, continuing startup")
//
//	        // Can also check the underlying cause
//	        if errors.Is(err, context.DeadlineExceeded) {
//	            log.Print("Timed out waiting for first fetch")
//	        }
//	        return nil
//	    }
//	    // Registration failed
//	    return fmt.Errorf("failed to register resource: %w", err)
//	}
//	// Resource registered AND ready with data
func ErrNotReady() error {
	return errNotReady
}
