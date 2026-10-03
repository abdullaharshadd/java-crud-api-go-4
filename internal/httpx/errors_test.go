package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	errmsg "migrated-app/internal/user/error_message"
)

// errsTestUserNotFound stands in for user.ErrUserNotFound. internal/user is
// not imported because the error handler receives the sentinel by injection.
var errsTestUserNotFound = errors.New("User are not available")

// errsTestNotFoundError mimics *user.NotFoundError: it matches the sentinel via Is.
type errsTestNotFoundError struct {
	msg   string
	cause error
}

func (e *errsTestNotFoundError) Error() string        { return e.msg }
func (e *errsTestNotFoundError) Unwrap() error        { return e.cause }
func (e *errsTestNotFoundError) Is(target error) bool { return target == errsTestUserNotFound }

func errsTestDecodeMap(t *testing.T, body string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("decode body %q: %v", body, err)
	}
	return m
}

func TestErrorHandlerWriteErrorUserNotFound(t *testing.T) {
	h := NewErrorHandler(errsTestUserNotFound)
	tests := []struct {
		name    string
		method  string
		path    string
		reqBody string
		err     error
		wantMsg string
	}{
		{"custom message", http.MethodGet, "/users/5", "", &errsTestNotFoundError{msg: "User not found with id 5"}, "User not found with id 5"},
		{"sentinel itself", http.MethodGet, "/users/1", "", errsTestUserNotFound, "User are not available"},
		{"empty message", http.MethodGet, "/users/2", "", &errsTestNotFoundError{msg: ""}, ""},
		{"wrapped sentinel", http.MethodDelete, "/users/3", "", fmt.Errorf("lookup: %w", errsTestUserNotFound), "lookup: User are not available"},
		{"post with body ignored", http.MethodPost, "/other/path", `{"x":1}`, &errsTestNotFoundError{msg: "nope"}, "nope"},
		{"put any path", http.MethodPut, "/a/b/c", "garbage", &errsTestNotFoundError{msg: "gone", cause: io.EOF}, "gone"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.reqBody))
			h.WriteError(rec, req, tc.err)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", rec.Code)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q", ct)
			}
			m := errsTestDecodeMap(t, rec.Body.String())
			if m["status"] != "NOT_FOUND" {
				t.Errorf("status field = %v", m["status"])
			}
			msg, ok := m["message"].(string)
			if !ok || msg != tc.wantMsg {
				t.Errorf("message = %v, want %q", m["message"], tc.wantMsg)
			}
			if len(m) != 2 {
				t.Errorf("unexpected fields: %v", m)
			}
			var em errmsg.ErrorMessage
			if err := json.Unmarshal(rec.Body.Bytes(), &em); err != nil {
				t.Fatalf("unmarshal ErrorMessage: %v", err)
			}
			if !em.Equal(errmsg.NewErrorMessage(errmsg.HTTPStatus(http.StatusNotFound), tc.wantMsg)) {
				t.Errorf("ErrorMessage = %v", em.String())
			}
			if strings.HasSuffix(rec.Body.String(), "\n") {
				t.Error("body has trailing newline")
			}
		})
	}
}

func TestErrorHandlerWriteErrorStatusErrors(t *testing.T) {
	h := NewErrorHandler(errsTestUserNotFound)
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantHeader map[string][]string
	}{
		{"malformed json", NewBadRequestError(errors.New("unexpected EOF")), 400, nil},
		{"validation failed", NewBadRequestError(errors.New("name required")), 400, nil},
		{"missing param", NewBadRequestError(nil), 400, nil},
		{"unsupported media", NewUnsupportedMediaTypeError(errors.New("text/xml"), "application/json"), 415, map[string][]string{"Accept": {"application/json"}}},
		{"unsupported media multi", NewUnsupportedMediaTypeError(nil, "application/json", "application/*+json"), 415, map[string][]string{"Accept": {"application/json", "application/*+json"}}},
		{"method not allowed", &StatusError{Status: 405, Header: http.Header{"Allow": {"GET, POST"}}}, 405, map[string][]string{"Allow": {"GET, POST"}}},
		{"wrapped status error", fmt.Errorf("ctx: %w", NewBadRequestError(nil)), 400, nil},
		{"plain status error", NewStatusError(406, nil), 406, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/users", nil)
			h.WriteError(rec, req, tc.err)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if rec.Body.Len() != 0 {
				t.Errorf("body = %q, want empty", rec.Body.String())
			}
			if ct := rec.Header().Get("Content-Type"); ct != "" {
				t.Errorf("Content-Type = %q, want none", ct)
			}
			for k, want := range tc.wantHeader {
				got := rec.Header().Values(k)
				if strings.Join(got, "|") != strings.Join(want, "|") {
					t.Errorf("header %s = %v, want %v", k, got, want)
				}
			}
		})
	}
}

func TestErrorHandlerUnhandledIsDefault500(t *testing.T) {
	h := NewErrorHandler(errsTestUserNotFound, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/boom", nil)
	h.WriteError(rec, req, errors.New("db down"))
	if rec.Code != 500 {
		t.Fatalf("status = %d", rec.Code)
	}
	var de DefaultError
	if err := json.Unmarshal(rec.Body.Bytes(), &de); err != nil {
		t.Fatal(err)
	}
	if de.Status != 500 || de.Error != "Internal Server Error" || de.Path != "/boom" {
		t.Errorf("body = %+v", de)
	}
	if _, err := time.Parse(defaultErrorTimeLayout, de.Timestamp); err != nil {
		t.Errorf("timestamp %q: %v", de.Timestamp, err)
	}
}

func TestErrorHandlerNoSentinelsAndNil(t *testing.T) {
	h := NewErrorHandler()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	h.WriteError(rec, req, errsTestUserNotFound)
	if rec.Code != 500 {
		t.Errorf("unregistered sentinel status = %d, want 500", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.WriteError(rec, req, nil)
	if rec.Body.Len() != 0 || len(rec.Header()) != 0 {
		t.Errorf("nil error wrote output: %q %v", rec.Body.String(), rec.Header())
	}
}

func TestErrorHandlerHandleAdapter(t *testing.T) {
	h := NewErrorHandler(errsTestUserNotFound)
	tests := []struct {
		name       string
		fn         HandlerFunc
		wantStatus int
		wantBody   string
	}{
		{"success", func(w http.ResponseWriter, r *http.Request) error { return WriteText(w, 200, "ok") }, 200, "ok"},
		{"not found", func(w http.ResponseWriter, r *http.Request) error {
			return &errsTestNotFoundError{msg: "User not found with id 5"}
		}, 404, `{"status":"NOT_FOUND","message":"User not found with id 5"}`},
		{"bad request", func(w http.ResponseWriter, r *http.Request) error { return NewBadRequestError(nil) }, 400, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.Handle(tc.fn).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/u", nil))
			if rec.Code != tc.wantStatus || rec.Body.String() != tc.wantBody {
				t.Errorf("got %d %q, want %d %q", rec.Code, rec.Body.String(), tc.wantStatus, tc.wantBody)
			}
		})
	}
}

func TestErrorsStatusErrorType(t *testing.T) {
	cause := errors.New("bad")
	tests := []struct {
		name string
		err  *StatusError
		want string
	}{
		{"with cause", NewBadRequestError(cause), "400 Bad Request: bad"},
		{"no cause", NewStatusError(405, nil), "405 Method Not Allowed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err.Error() != tc.want {
				t.Errorf("Error() = %q", tc.err.Error())
			}
		})
	}
	if !errors.Is(NewBadRequestError(cause), cause) {
		t.Error("Unwrap should expose cause")
	}
	if e := NewUnsupportedMediaTypeError(nil); e.Header != nil || e.Status != 415 {
		t.Errorf("got %+v", e)
	}
}

func TestErrorsWriteHelpers(t *testing.T) {
	rec := httptest.NewRecorder()
	if err := WriteJSON(rec, 200, make(chan int)); err == nil {
		t.Error("expected encode error")
	}
	if rec.Body.Len() != 0 || rec.Header().Get("Content-Type") != "" {
		t.Error("nothing should be written on encode failure")
	}

	rec = httptest.NewRecorder()
	if err := WriteText(rec, 201, "hi"); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 201 || rec.Body.String() != "hi" || rec.Header().Get("Content-Type") != "text/plain;charset=UTF-8" {
		t.Errorf("WriteText: %d %q %v", rec.Code, rec.Body.String(), rec.Header())
	}

	rec = httptest.NewRecorder()
	rec.Header().Set("Content-Type", "x")
	WriteEmpty(rec, 400)
	if rec.Code != 400 || rec.Header().Get("Content-Type") != "" || rec.Header().Get("Content-Length") != "0" {
		t.Errorf("WriteEmpty: %d %v", rec.Code, rec.Header())
	}
}

func TestErrorsRecoverMiddleware(t *testing.T) {
	rec := httptest.NewRecorder()
	Recover(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { panic("kaboom") })).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/p", nil))
	if rec.Code != 500 {
		t.Fatalf("status = %d", rec.Code)
	}
	if m := errsTestDecodeMap(t, rec.Body.String()); m["path"] != "/p" {
		t.Errorf("body = %v", m)
	}

	rec = httptest.NewRecorder()
	Recover(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/p", nil))
	if rec.Code != 204 {
		t.Errorf("status = %d", rec.Code)
	}

	defer func() {
		if r := recover(); r != http.ErrAbortHandler {
			t.Errorf("recovered %v, want ErrAbortHandler", r)
		}
	}()
	Recover(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { panic(http.ErrAbortHandler) })).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/p", nil))
}

func TestErrorsWithDefaultErrorsMux(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /users", func(w http.ResponseWriter, r *http.Request) { _ = WriteText(w, 200, "list") })
	mux.HandleFunc("/dir/", func(w http.ResponseWriter, r *http.Request) { _ = WriteText(w, 200, "dir") })
	h := WithDefaultErrors(mux)

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		check      func(t *testing.T, rec *httptest.ResponseRecorder)
	}{
		{"matched", http.MethodGet, "/users", 200, func(t *testing.T, rec *httptest.ResponseRecorder) {
			if rec.Body.String() != "list" {
				t.Errorf("body = %q", rec.Body.String())
			}
		}},
		{"unknown path", http.MethodGet, "/nope", 404, func(t *testing.T, rec *httptest.ResponseRecorder) {
			m := errsTestDecodeMap(t, rec.Body.String())
			if m["error"] != "Not Found" || m["path"] != "/nope" || m["status"] != float64(404) {
				t.Errorf("body = %v", m)
			}
		}},
		{"wrong method", http.MethodDelete, "/users", 405, func(t *testing.T, rec *httptest.ResponseRecorder) {
			if rec.Body.Len() != 0 {
				t.Errorf("body = %q", rec.Body.String())
			}
			if allow := rec.Header().Get("Allow"); !strings.Contains(allow, "GET") {
				t.Errorf("Allow = %q", allow)
			}
		}},
		{"redirect passes through", http.MethodGet, "/dir", http.StatusMovedPermanently, func(t *testing.T, rec *httptest.ResponseRecorder) {
			if loc := rec.Header().Get("Location"); !strings.HasSuffix(loc, "/dir/") {
				t.Errorf("Location = %q", loc)
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			tc.check(t, rec)
		})
	}
}