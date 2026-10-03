// Package user contains the user domain: model, persistence, service logic
// and the errors those layers report.
package user

import "errors"

// UserNotFoundMessage is the exact message returned to clients when a user
// lookup fails. It is part of the public API contract (it appears in the
// 404 JSON body), so do not change its wording.
const UserNotFoundMessage = "User are not available"

// ErrUserNotFound is the sentinel reported when a requested user does not
// exist. HTTP handlers map it to 404 Not Found with an ErrorMessage body
// carrying err.Error().
//
// It replaces com.smartContact.error.UserNotFoundException. Match it with
// errors.Is(err, ErrUserNotFound); this also matches any *NotFoundError.
var ErrUserNotFound = errors.New(UserNotFoundMessage)

// ErrValidation is reported when a request payload fails validation.
var ErrValidation = errors.New("validation failed")

// ErrNotUnique is reported when a lookup that expects at most one row (for
// example, find-by-name) matches several rows.
var ErrNotUnique = errors.New("query did not return a unique result")

// ErrNothingDeleted is reported when a delete-by-id hits no row, which
// mirrors Spring Data's EmptyResultDataAccessException from deleteById.
var ErrNothingDeleted = errors.New("no user entity exists with the given id")

// NotFoundError is a user-not-found error that carries a custom message
// and/or an underlying cause. errors.Is(err, ErrUserNotFound) holds for
// every *NotFoundError, and errors.Unwrap exposes Cause.
//
// It covers the overloaded constructors of the Java UserNotFoundException:
//
//	new UserNotFoundException()                -> &NotFoundError{} (or just ErrUserNotFound)
//	new UserNotFoundException(msg)             -> NewNotFoundError(msg, nil)
//	new UserNotFoundException(msg, cause)      -> NewNotFoundError(msg, cause)
//	new UserNotFoundException(cause)           -> NewNotFoundError("", cause)
//
// MIGRATION_NOTE: the protected constructor taking enableSuppression and
// writableStackTrace has no Go equivalent. Go errors have no suppressed
// exceptions or captured stack traces, so it was dropped on purpose.
type NotFoundError struct {
	// Message is the detail message. An empty Message means none was set.
	Message string
	// Cause is the underlying error, if any.
	Cause error
}

// NewNotFoundError returns a *NotFoundError with the given detail message
// and optional cause. Pass an empty message to derive it from the cause,
// and a nil cause when there is none.
func NewNotFoundError(message string, cause error) *NotFoundError {
	return &NotFoundError{Message: message, Cause: cause}
}

// Error returns the detail message. Without one it falls back to the
// cause's message, the same way Java's Throwable(Throwable cause) builds
// its message. With neither set it returns the standard not-found message.
func (e *NotFoundError) Error() string {
	switch {
	case e.Message != "":
		return e.Message
	case e.Cause != nil:
		return e.Cause.Error()
	default:
		return UserNotFoundMessage
	}
}

// Unwrap returns the underlying cause, or nil.
func (e *NotFoundError) Unwrap() error { return e.Cause }

// Is reports whether target is ErrUserNotFound, so errors.Is treats every
// *NotFoundError as a user-not-found error.
func (e *NotFoundError) Is(target error) bool { return target == ErrUserNotFound }
