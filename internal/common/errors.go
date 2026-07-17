package common

import "errors"

type ErrorCode string

const (
	CodeInvalidRequest       ErrorCode = "invalid_request"
	CodeNotFound             ErrorCode = "not_found"
	CodeContentTooLarge      ErrorCode = "content_too_large"
	CodeUnsupportedMediaType ErrorCode = "unsupported_media_type"
	CodeStorageUnavailable   ErrorCode = "storage_unavailable"
	CodeInternal             ErrorCode = "internal_error"
)

type Error struct {
	Code    ErrorCode
	Message string
	Err     error
}

var ErrIDCollision = errors.New("resource id collision")

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.Err }

func NewError(code ErrorCode, message string, cause error) *Error {
	return &Error{Code: code, Message: message, Err: cause}
}

func HasCode(err error, code ErrorCode) bool {
	var target *Error
	return errors.As(err, &target) && target.Code == code
}
