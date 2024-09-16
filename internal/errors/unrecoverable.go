package errors

// Unrecoverable represents an error that can not be recovered
// from without user intervention. When this error is returned
// the request should not be requeued.
type Unrecoverable struct {
	error
}

func NewUnrecoverable(err error) Unrecoverable {
	return Unrecoverable{err}
}
