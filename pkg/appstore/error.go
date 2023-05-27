package appstore

type Error struct {
	Metadata        interface{}
	underlyingError error
}

func (t Error) Error() string {
	return t.underlyingError.Error()
}

func NewErrorWithMetadata(err error, metadata interface{}) *Error {
	return &Error{
		underlyingError: err,
		Metadata:        metadata,
	}
}
