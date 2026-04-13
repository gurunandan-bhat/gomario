package service

// HTTPError is a sentinel error type that carries an HTTP status code.
// Handlers return an HTTPError to signal the intended response status and
// a safe user-facing message. Any other error type is treated as an
// unexpected 500.
type HTTPError struct {
	Code    int
	Message string
}

func (e HTTPError) Error() string { return e.Message }

func ErrBadRequest(msg string) HTTPError   { return HTTPError{400, msg} }
func ErrUnauthorized(msg string) HTTPError { return HTTPError{401, msg} }
func ErrForbidden(msg string) HTTPError    { return HTTPError{403, msg} }
func ErrNotFound(msg string) HTTPError     { return HTTPError{404, msg} }
func ErrMethodNotAllowed(msg string) HTTPError { return HTTPError{405, msg} }
