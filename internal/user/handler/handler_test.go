package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"migrated-app/internal/httpx"
	"migrated-app/internal/user/repository"
)

var errTestNotFound = errors.New("User are not available")

type call struct {
	method string
	id     int
	name   string
	user   *repository.User
}

type mockService struct {
	calls []call

	saveErr   error
	list      []*repository.User
	listErr   error
	byID      *repository.User
	byIDErr   error
	deleteErr error
	updateErr error
	byName    *repository.User
	byNameOK  bool
	byNameErr error
}

func (m *mockService) SaveUser(_ context.Context, u *repository.User) (*repository.User, error) {
	m.calls = append(m.calls, call{method: "SaveUser", user: u})
	if m.saveErr != nil {
		return nil, m.saveErr
	}
	return u, nil
}

func (m *mockService) FetchUserList(_ context.Context) ([]*repository.User, error) {
	m.calls = append(m.calls, call{method: "FetchUserList"})
	return m.list, m.listErr
}

func (m *mockService) FetchUserByID(_ context.Context, id int) (*repository.User, error) {
	m.calls = append(m.calls, call{method: "FetchUserByID", id: id})
	return m.byID, m.byIDErr
}

func (m *mockService) DeleteUser(_ context.Context, id int) error {
	m.calls = append(m.calls, call{method: "DeleteUser", id: id})
	return m.deleteErr
}

func (m *mockService) UpdateUser(_ context.Context, id int, u *repository.User) error {
	m.calls = append(m.calls, call{method: "UpdateUser", id: id, user: u})
	return m.updateErr
}

func (m *mockService) GetUserNameByName(_ context.Context, name string) (*repository.User, bool, error) {
	m.calls = append(m.calls, call{method: "GetUserNameByName", name: name})
	return m.byName, m.byNameOK, m.byNameErr
}

func mustUser(t *testing.T, js string) *repository.User {
	t.Helper()
	var u repository.User
	if err := json.Unmarshal([]byte(js), &u); err != nil {
		t.Fatalf("unmarshal user: %v", err)
	}
	return &u
}

func normalize(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

func normalizeBody(t *testing.T, body []byte) any {
	t.Helper()
	var out any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("response not JSON: %v (%q)", err, body)
	}
	return out
}

func newServer(t *testing.T, svc *mockService) *http.ServeMux {
	t.Helper()
	h, err := New(svc, httpx.NewErrorHandler(errTestNotFound))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mux := http.NewServeMux()
	h.Register(mux)
	return mux
}

func do(mux http.Handler, method, path, ct, body string) *httptest.ResponseRecorder {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, bytes.NewBufferString(body))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	if ct != "" {
		r.Header.Set("Content-Type", ct)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func countCalls(svc *mockService, method string) int {
	n := 0
	for _, c := range svc.calls {
		if c.method == method {
			n++
		}
	}
	return n
}

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		svc     Service
		errs    *httpx.ErrorHandler
		wantErr bool
	}{
		{"ok", &mockService{}, httpx.NewErrorHandler(errTestNotFound), false},
		{"nil service", nil, httpx.NewErrorHandler(errTestNotFound), true},
		{"nil error handler", &mockService{}, nil, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, err := New(tc.svc, tc.errs)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tc.wantErr)
			}
			if !tc.wantErr && h == nil {
				t.Fatal("nil handler")
			}
		})
	}
}

func TestSaveUser(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		ct        string
		body      string
		saveErr   error
		wantCode  int
		wantBody  string
		wantCalls int
	}{
		{"valid", "/save_user_data", "application/json", `{"name":"alice","email":"a@x"}`, nil, 200, msgSaved, 1},
		{"trailing slash", "/save_user_data/", "application/json", `{"name":"alice"}`, nil, 200, msgSaved, 1},
		{"json with charset", "/save_user_data", "application/json; charset=utf-8", `{"name":"bob"}`, nil, 200, msgSaved, 1},
		{"+json content type", "/save_user_data", "application/vnd.api+json", `{"name":"bob"}`, nil, 200, msgSaved, 1},
		{"unknown fields ignored", "/save_user_data", "application/json", `{"name":"bob","extra":1}`, nil, 200, msgSaved, 1},
		{"missing name", "/save_user_data", "application/json", `{"email":"a@x"}`, nil, 400, "", 0},
		{"null name", "/save_user_data", "application/json", `{"name":null}`, nil, 400, "", 0},
		{"blank name", "/save_user_data", "application/json", `{"name":"  \t "}`, nil, 400, "", 0},
		{"empty name", "/save_user_data", "application/json", `{"name":""}`, nil, 400, "", 0},
		{"malformed json", "/save_user_data", "application/json", `{"name":`, nil, 400, "", 0},
		{"empty body", "/save_user_data", "application/json", ``, nil, 400, "", 0},
		{"whitespace body", "/save_user_data", "application/json", `   `, nil, 400, "", 0},
		{"null body", "/save_user_data", "application/json", `null`, nil, 400, "", 0},
		{"wrong type", "/save_user_data", "application/json", `[1,2]`, nil, 400, "", 0},
		{"missing content type", "/save_user_data", "", `{"name":"alice"}`, nil, 415, "", 0},
		{"text content type", "/save_user_data", "text/plain", `{"name":"alice"}`, nil, 415, "", 0},
		{"service error", "/save_user_data", "application/json", `{"name":"alice"}`, errors.New("db down"), 500, "", 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := &mockService{saveErr: tc.saveErr}
			w := do(newServer(t, svc), http.MethodPost, tc.path, tc.ct, tc.body)
			if w.Code != tc.wantCode {
				t.Fatalf("code=%d want %d body=%q", w.Code, tc.wantCode, w.Body.String())
			}
			if tc.wantBody != "" && w.Body.String() != tc.wantBody {
				t.Fatalf("body=%q want %q", w.Body.String(), tc.wantBody)
			}
			if tc.wantCode != 200 && strings.Contains(w.Body.String(), msgSaved) {
				t.Fatal("success message returned on failure")
			}
			if got := countCalls(svc, "SaveUser"); got != tc.wantCalls {
				t.Fatalf("SaveUser calls=%d want %d", got, tc.wantCalls)
			}
			if tc.wantCalls == 1 {
				got := normalize(t, svc.calls[0].user)
				want := normalize(t, mustUser(t, tc.body))
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("service got %v want %v", got, want)
				}
			}
		})
	}
}

func TestFetchUserList(t *testing.T) {
	u1 := mustUser(t, `{"id":2,"name":"b"}`)
	u2 := mustUser(t, `{"id":1,"name":"a"}`)
	tests := []struct {
		name     string
		path     string
		list     []*repository.User
		listErr  error
		wantCode int
		want     any
	}{
		{"users exist preserves order", "/get_user_data", []*repository.User{u1, u2}, nil, 200, []*repository.User{u1, u2}},
		{"trailing slash", "/get_user_data/", []*repository.User{u1}, nil, 200, []*repository.User{u1}},
		{"empty", "/get_user_data", []*repository.User{}, nil, 200, []any{}},
		{"nil becomes empty", "/get_user_data", nil, nil, 200, []any{}},
		{"service error", "/get_user_data", nil, errors.New("boom"), 500, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := &mockService{list: tc.list, listErr: tc.listErr}
			w := do(newServer(t, svc), http.MethodGet, tc.path, "", "")
			if w.Code != tc.wantCode {
				t.Fatalf("code=%d want %d", w.Code, tc.wantCode)
			}
			if countCalls(svc, "FetchUserList") != 1 {
				t.Fatalf("calls=%v", svc.calls)
			}
			if tc.wantCode == 200 {
				if strings.TrimSpace(w.Body.String()) == "null" {
					t.Fatal("got null, want array")
				}
				got := normalizeBody(t, w.Body.Bytes())
				if !reflect.DeepEqual(got, normalize(t, tc.want)) {
					t.Fatalf("body=%v want %v", got, normalize(t, tc.want))
				}
			}
		})
	}
}

func TestFetchUserByID(t *testing.T) {
	u := mustUser(t, `{"id":7,"name":"seven"}`)
	tests := []struct {
		name      string
		path      string
		user      *repository.User
		err       error
		wantCode  int
		wantCalls int
		wantID    int
	}{
		{"found", "/get_user_data/7", u, nil, 200, 1, 7},
		{"found trailing slash", "/get_user_data/7/", u, nil, 200, 1, 7},
		{"negative id", "/get_user_data/-3", u, nil, 200, 1, -3},
		{"not found", "/get_user_data/9", nil, errTestNotFound, 404, 1, 9},
		{"other error", "/get_user_data/9", nil, errors.New("boom"), 500, 1, 9},
		{"non-integer", "/get_user_data/abc", nil, nil, 400, 0, 0},
		{"float", "/get_user_data/1.5", nil, nil, 400, 0, 0},
		{"overflow int32", "/get_user_data/2147483648", nil, nil, 400, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := &mockService{byID: tc.user, byIDErr: tc.err}
			w := do(newServer(t, svc), http.MethodGet, tc.path, "", "")
			if w.Code != tc.wantCode {
				t.Fatalf("code=%d want %d body=%q", w.Code, tc.wantCode, w.Body.String())
			}
			if got := countCalls(svc, "FetchUserByID"); got != tc.wantCalls {
				t.Fatalf("calls=%d want %d", got, tc.wantCalls)
			}
			if tc.wantCalls == 1 && svc.calls[0].id != tc.wantID {
				t.Fatalf("id=%d want %d", svc.calls[0].id, tc.wantID)
			}
			if tc.wantCode == 200 {
				if !reflect.DeepEqual(normalizeBody(t, w.Body.Bytes()), normalize(t, tc.user)) {
					t.Fatalf("body=%q", w.Body.String())
				}
			}
		})
	}
}

func TestDeleteUser(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		err       error
		wantCode  int
		wantCalls int
		wantID    int
	}{
		{"success", "/delete_user_data/5", nil, 200, 1, 5},
		{"success trailing slash", "/delete_user_data/5/", nil, 200, 1, 5},
		{"service error", "/delete_user_data/5", errors.New("no user entity"), 500, 1, 5},
		{"non-integer", "/delete_user_data/x", nil, 400, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := &mockService{deleteErr: tc.err}
			w := do(newServer(t, svc), http.MethodDelete, tc.path, "", "")
			if w.Code != tc.wantCode {
				t.Fatalf("code=%d want %d", w.Code, tc.wantCode)
			}
			if got := countCalls(svc, "DeleteUser"); got != tc.wantCalls {
				t.Fatalf("calls=%d want %d", got, tc.wantCalls)
			}
			if tc.wantCalls == 1 && svc.calls[0].id != tc.wantID {
				t.Fatalf("id=%d", svc.calls[0].id)
			}
			if tc.wantCode == 200 && w.Body.String() != msgDeleted {
				t.Fatalf("body=%q want %q", w.Body.String(), msgDeleted)
			}
			if tc.wantCode != 200 && strings.Contains(w.Body.String(), msgDeleted) {
				t.Fatal("success message on failure")
			}
		})
	}
}

func TestUpdateUser(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		ct        string
		body      string
		err       error
		wantCode  int
		wantCalls int
		wantID    int
		wantEcho  string
	}{
		{"success echoes body with path id", "/update_user_data/4", "application/json", `{"id":99,"name":"n","email":"e"}`, nil, 200, 1, 4, `{"id":4,"name":"n","email":"e"}`},
		{"trailing slash", "/update_user_data/4/", "application/json", `{"name":"n"}`, nil, 200, 1, 4, `{"id":4,"name":"n"}`},
		{"no validation blank name", "/update_user_data/3", "application/json", `{"name":""}`, nil, 200, 1, 3, `{"id":3,"name":""}`},
		{"no validation missing name", "/update_user_data/3", "application/json", `{"email":"e"}`, nil, 200, 1, 3, `{"id":3,"email":"e"}`},
		{"service error", "/update_user_data/3", "application/json", `{"name":"n"}`, errors.New("boom"), 500, 1, 3, ""},
		{"non-integer id", "/update_user_data/z", "application/json", `{"name":"n"}`, nil, 400, 0, 0, ""},
		{"malformed body", "/update_user_data/3", "application/json", `{`, nil, 400, 0, 0, ""},
		{"empty body", "/update_user_data/3", "application/json", ``, nil, 400, 0, 0, ""},
		{"null body", "/update_user_data/3", "application/json", `null`, nil, 400, 0, 0, ""},
		{"bad content type", "/update_user_data/3", "text/plain", `{"name":"n"}`, nil, 415, 0, 0, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := &mockService{updateErr: tc.err}
			w := do(newServer(t, svc), http.MethodPut, tc.path, tc.ct, tc.body)
			if w.Code != tc.wantCode {
				t.Fatalf("code=%d want %d body=%q", w.Code, tc.wantCode, w.Body.String())
			}
			if got := countCalls(svc, "UpdateUser"); got != tc.wantCalls {
				t.Fatalf("calls=%d want %d", got, tc.wantCalls)
			}
			if tc.wantCalls == 1 {
				c := svc.calls[0]
				if c.id != tc.wantID || c.user == nil {
					t.Fatalf("call=%+v", c)
				}
			}
			if tc.wantEcho != "" {
				got := normalizeBody(t, w.Body.Bytes())
				want := normalize(t, mustUser(t, tc.wantEcho))
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("body=%v want %v", got, want)
				}
			}
		})
	}
}

func TestGetUserNameByName(t *testing.T) {
	u := mustUser(t, `{"id":1,"name":"alice"}`)
	tests := []struct {
		name      string
		path      string
		user      *repository.User
		ok        bool
		err       error
		wantCode  int
		wantName  string
		wantEmpty bool
	}{
		{"found", "/get_user_name/name/alice", u, true, nil, 200, "alice", false},
		{"found trailing slash", "/get_user_name/name/alice/", u, true, nil, 200, "alice", false},
		{"escaped name", "/get_user_name/name/john%20doe", u, true, nil, 200, "john doe", false},
		{"no match", "/get_user_name/name/bob", nil, false, nil, 200, "bob", true},
		{"ok but nil user", "/get_user_name/name/bob", nil, true, nil, 200, "bob", true},
		{"service error", "/get_user_name/name/bob", nil, false, errors.New("not unique"), 500, "bob", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := &mockService{byName: tc.user, byNameOK: tc.ok, byNameErr: tc.err}
			w := do(newServer(t, svc), http.MethodGet, tc.path, "", "")
			if w.Code != tc.wantCode {
				t.Fatalf("code=%d want %d", w.Code, tc.wantCode)
			}
			if countCalls(svc, "GetUserNameByName") != 1 || svc.calls[0].name != tc.wantName {
				t.Fatalf("calls=%+v", svc.calls)
			}
			if tc.wantEmpty {
				if w.Body.Len() != 0 {
					t.Fatalf("body=%q want empty", w.Body.String())
				}
				if ct := w.Header().Get("Content-Type"); ct != "" {
					t.Fatalf("content-type=%q want none", ct)
				}
			} else if tc.wantCode == 200 {
				if !reflect.DeepEqual(normalizeBody(t, w.Body.Bytes()), normalize(t, u)) {
					t.Fatalf("body=%q", w.Body.String())
				}
			}
		})
	}
}

func TestRoutesMethodMismatch(t *testing.T) {
	tests := []struct {
		method, path string
	}{
		{http.MethodGet, "/save_user_data"},
		{http.MethodPost, "/get_user_data"},
		{http.MethodGet, "/delete_user_data/1"},
		{http.MethodGet, "/update_user_data/1"},
	}
	for _, tc := range tests {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			svc := &mockService{}
			w := do(newServer(t, svc), tc.method, tc.path, "", "")
			if w.Code != http.StatusMethodNotAllowed {
				t.Fatalf("code=%d want 405", w.Code)
			}
			if len(svc.calls) != 0 {
				t.Fatalf("unexpected calls %+v", svc.calls)
			}
		})
	}
}

func TestIsJSONContentType(t *testing.T) {
	tests := []struct {
		ct   string
		want bool
	}{
		{"", false},
		{"application/json", true},
		{"APPLICATION/JSON", true},
		{"application/json; charset=utf-8", true},
		{"application/problem+json", true},
		{"text/json", false},
		{"text/plain", false},
		{"application/octet-stream", false},
		{";;bad", false},
	}
	for _, tc := range tests {
		t.Run(tc.ct, func(t *testing.T) {
			if got := isJSONContentType(tc.ct); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestValidateUser(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{"valid", `{"name":"a"}`, false},
		{"padded valid", `{"name":"  a  "}`, false},
		{"missing", `{}`, true},
		{"null", `{"name":null}`, true},
		{"blank", `{"name":" \n\t"}`, true},
		{"bad json", `{`, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateUser([]byte(tc.raw))
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tc.wantErr)
			}
			if err != nil && tc.raw != `{` && !strings.Contains(err.Error(), nameRequiredMessage) {
				t.Fatalf("err=%v missing message", err)
			}
		})
	}
}