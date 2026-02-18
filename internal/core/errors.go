package core

import "fmt"

type ErrorCode string

const (
	ErrBadRequest            ErrorCode = "WVS_BAD_REQUEST"
	ErrNotFound              ErrorCode = "WVS_NOT_FOUND"
	ErrConflictLocked        ErrorCode = "WVS_CONFLICT_LOCKED"
	ErrConflictIdempotent    ErrorCode = "WVS_CONFLICT_IDEMPOTENT_MISMATCH"
	ErrConflictExists        ErrorCode = "WVS_CONFLICT_EXISTS"
	ErrConflictSnapshotInUse ErrorCode = "WVS_CONFLICT_SNAPSHOT_IN_USE"
	ErrGone                  ErrorCode = "WVS_GONE"
	ErrPreconditionFailed    ErrorCode = "WVS_PRECONDITION_FAILED"
	ErrInternal              ErrorCode = "WVS_INTERNAL"
	ErrExecutorError         ErrorCode = "WVS_EXECUTOR_ERROR"
	ErrExecutorTimeout       ErrorCode = "WVS_EXECUTOR_TIMEOUT"
)

// HTTPStatus returns the HTTP status code for this error code.
func (e ErrorCode) HTTPStatus() int {
	switch e {
	case ErrBadRequest:
		return 400
	case ErrNotFound:
		return 404
	case ErrConflictLocked, ErrConflictIdempotent, ErrConflictExists, ErrConflictSnapshotInUse:
		return 409
	case ErrGone:
		return 410
	case ErrPreconditionFailed:
		return 412
	case ErrExecutorError:
		return 502
	case ErrExecutorTimeout:
		return 504
	default:
		return 500
	}
}

type AppError struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

func (e *AppError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func NewAppError(code ErrorCode, msg string) *AppError {
	return &AppError{Code: code, Message: msg}
}
