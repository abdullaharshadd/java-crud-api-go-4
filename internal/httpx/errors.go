// Package httpx holds the cross-cutting HTTP plumbing that Spring MVC supplied
// implicitly: response writers, the global error mapping that replaces
// com.smartContact.error.RestResponseEntityExceptionHandling (a
// @ControllerAdvice extending ResponseEntityExceptionHandler), Spring Boot's
// default error JSON, and the middleware that turns unknown routes, wrong
// methods and panics into the same responses Boot produced.
package httpx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"

	errmsg "migrated-app/internal/user/error_message"
)

// defaultErrorTimeLayout matches Jackson's default rendering of
// java.util.Date in Spring Boot 2.7's DefaultErrorAttributes.
const defaultErrorTimeLayout = "2006-01-02T15:04:05.000-07:00"

const (
	contentTypeJSON = "application/json"
	contentTypeText = "text/plain;charset=UTF-8"
)

// WriteJSON encodes v as JSON and writes it with the given status. The body
// is encoded before any header is sent, so an encoding failure becomes a
// 500 default error instead of a truncated response.
func WriteJSON(w http.ResponseWriter, status int, v any) error {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(v); err != nil {
		return fmt.Errorf("httpx: encode JSON response: %w", err)
	}
	// json.Encoder appends a newline; Jackson does not.
	body := bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
	w.Header().Set("Content-Type", contentTypeJSON)
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("httpx: write JSON response: %w", err)
	}
	return nil
}

// WriteText writes s as text/plain;charset=UTF-8 with the given status,
// matching a Spring controller returning a String.
func WriteText(w http.ResponseWriter, status int, s string) error {
	w.Header().Set("Content-Type", contentTypeText)
	w.WriteHeader(status)
	if _, err := w.Write([]byte(s)); err != nil {
		return fmt.Errorf("httpx: write text response: %w", err)
	}
	return nil
}

// WriteEmpty writes status with no body and no Content-Type. It mirrors
// ResponseEntityExceptionHandler, which answers standard framework
// exceptions (400, 405, 406, 415, ...) with a null body.
func WriteEmpty(w http.ResponseWriter, status int) {
	w.Header().Del("Content-Type")
	w.Header().Set("Content-Length", "0")
	w.WriteHeader(status)
}

// DefaultError is Spring Boot 2.7's default error body
// (DefaultErrorAttributes without the message attribute).
type DefaultError struct {
	Timestamp string `json:"timestamp"`
	Status    int    `json:"status"`
	Error     string `json:"error"`
	Path      string `json:"path"`
}

// WriteDefaultError writes Boot's default error JSON for status, e.g.
// {"timestamp":"...","status":404,"error":"Not Found","path":"/x"}.
func WriteDefaultError(w http.ResponseWriter, r *http.Request, status int) {
	body := DefaultError{
		Timestamp: time.Now().Format(defaultErrorTimeLayout),
		Status:    status,
		Error:     http.StatusText(status),
		Path:      r.URL.Path,
	}
	if err := WriteJSON(w, status, body); err != nil {
		log.Error().Err(err).Str("path", r.URL.Path).Msg("write default error response")
	}
}

// StatusError is a standard framework-level failure (malformed body, type
// mismatch, missing parameter, unsupported media type, ...). It is the Go
// counterpart of the Spring MVC exceptions handled by
// ResponseEntityExceptionHandler, and is answered with its Status, any
// extra Header values, and an empty body.
type StatusError struct {
	// Status is the HTTP status code to respond with.
	Status int
	// Header holds response headers to add, e.g. Allow for 405 or Accept
	// for 415. It may be nil.
	Header http.Header
	// Cause is the underlying error, if any.
	Cause error
}

// NewStatusError returns a *StatusError for status wrapping cause.
func NewStatusError(status int, cause error) *StatusError {
	return &StatusError{Status: status, Cause: cause}
}

// NewBadRequestError returns a 400 *StatusError, the equivalent of
// HttpMessageNotReadableException, MethodArgumentNotValidException,
// TypeMismatchException, MissingServletRequestParameterException and
// friends.
func NewBadRequestError(cause error) *StatusError {
	return NewStatusError(http.StatusBadRequest, cause)
}

// NewUnsupportedMediaTypeError returns a 415 *StatusError carrying an Accept
// header, the equivalent of HttpMediaTypeNotSupportedException.
func NewUnsupportedMediaTypeError(cause error, accepted ...string) *StatusError {
	e := NewStatusError(http.StatusUnsupportedMediaType, cause)
	if len(accepted) > 0 {
		e.Header = http.Header{}
		for _, a := range accepted {
			e.Header.Add("Accept", a)
		}
	}
	return e
}

// Error describes the failure.
func (e *StatusError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%d %s: %v", e.Status, http.StatusText(e.Status), e.Cause)
	}
	return fmt.Sprintf("%d %s", e.Status, http.StatusText(e.Status))
}

// Unwrap returns the underlying cause, or nil.
func (e *StatusError) Unwrap() error { return e.Cause }

// ErrorHandler maps errors returned by HTTP handlers to responses. It
// replaces the @ControllerAdvice RestResponseEntityExceptionHandling.
//
// MIGRATION_NOTE: the user-not-found sentinel is injected rather than
// imported because internal/user currently does not compile (model.go and
// errors.go declare different package names). Wire it in main with
// httpx.NewErrorHandler(user.ErrUserNotFound). Every *user.NotFoundError
// matches that sentinel through its Is method.
type ErrorHandler struct {
	notFound []error
}

// NewErrorHandler returns an ErrorHandler that answers any error matching
// one of notFound (via errors.Is) with 404 and an ErrorMessage body.
func NewErrorHandler(notFound ...error) *ErrorHandler {
	return &ErrorHandler{notFound: notFound}
}

// isNotFound reports whether err matches a registered not-found sentinel.
func (h *ErrorHandler) isNotFound(err error) bool {
	for _, target := range h.notFound {
		if target != nil && errors.Is(err, target) {
			return true
		}
	}
	return false
}

// WriteError writes the response for err:
//   - user not found: 404 {"status":"NOT_FOUND","message":err.Error()}
//   - *StatusError: its status and headers with an empty body
//   - anything else: 500 Boot default error JSON
func (h *ErrorHandler) WriteError(w http.ResponseWriter, r *http.Request, err error) {
	if err == nil {
		return
	}
	if h.isNotFound(err) {
		body := errmsg.NewErrorMessage(errmsg.HTTPStatus(http.StatusNotFound), err.Error())
		if werr := WriteJSON(w, http.StatusNotFound, body); werr != nil {
			log.Error().Err(werr).Str("path", r.URL.Path).Msg("write not-found response")
		}
		return
	}
	var se *StatusError
	if errors.As(err, &se) {
		for k, vs := range se.Header {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		WriteEmpty(w, se.Status)
		return
	}
	log.Error().Err(err).Str("method", r.Method).Str("path", r.URL.Path).Msg("unhandled handler error")
	WriteDefaultError(w, r, http.StatusInternalServerError)
}

// HandlerFunc is an HTTP handler that reports failure by returning an error,
// the Go counterpart of a controller method that throws.
type HandlerFunc func(w http.ResponseWriter, r *http.Request) error

// Handle adapts fn to http.Handler, routing any returned error through
// WriteError.
func (h *ErrorHandler) Handle(fn HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			h.WriteError(w, r, err)
		}
	})
}

// Recover turns a panic in next into a 500 default error, as Boot's error
// controller does for an uncaught exception. http.ErrAbortHandler is
// re-panicked so net/http can abort the connection as intended.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			if rec == http.ErrAbortHandler {
				panic(rec)
			}
			log.Error().Interface("panic", rec).Str("method", r.Method).Str("path", r.URL.Path).Msg("recovered from panic")
			WriteDefaultError(w, r, http.StatusInternalServerError)
		}()
		next.ServeHTTP(w, r)
	})
}

// interceptWriter captures the status and headers the ServeMux writes for an
// unmatched request and discards its plain-text body.
type interceptWriter struct {
	header http.Header
	status int
}

func (iw *interceptWriter) Header() http.Header { return iw.header }

func (iw *interceptWriter) Write(b []byte) (int, error) {
	if iw.status == 0 {
		iw.status = http.StatusOK
	}
	return len(b), nil
}

func (iw *interceptWriter) WriteHeader(status int) {
	if iw.status == 0 {
		iw.status = status
	}
}

// WithDefaultErrors wraps mux so that requests it cannot route get Spring's
// responses instead of net/http's plain-text ones:
//   - unknown path: 404 Boot default error JSON
//   - known path, wrong method: 405 with an Allow header and an empty body
//
// Redirects issued by the mux (e.g. trailing-slash) pass through unchanged.
func WithDefaultErrors(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h, pattern := mux.Handler(r)
		if pattern != "" {
			mux.ServeHTTP(w, r)
			return
		}
		iw := &interceptWriter{header: http.Header{}}
		h.ServeHTTP(iw, r)
		switch iw.status {
		case http.StatusMethodNotAllowed:
			for _, v := range iw.header.Values("Allow") {
				w.Header().Add("Allow", v)
			}
			WriteEmpty(w, http.StatusMethodNotAllowed)
		case http.StatusNotFound, 0:
			WriteDefaultError(w, r, http.StatusNotFound)
		default:
			for k, vs := range iw.header {
				if k == "Content-Type" || k == "Content-Length" {
					continue
				}
				for _, v := range vs {
					w.Header().Add(k, v)
				}
			}
			w.WriteHeader(iw.status)
		}
	})
}
