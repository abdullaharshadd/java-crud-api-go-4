package user

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewEmptyErrorMessage(t *testing.T) {
	m := NewEmptyErrorMessage()
	if m == nil {
		t.Fatal("expected non-nil")
	}
	if m.GetStatus() != 0 {
		t.Errorf("status = %v, want unset", m.GetStatus())
	}
	if m.GetMessage() != "" {
		t.Errorf("message = %q, want empty", m.GetMessage())
	}
}

func TestNewErrorMessage(t *testing.T) {
	tests := []struct {
		name    string
		status  HTTPStatus
		message string
	}{
		{"not found", HTTPStatus(http.StatusNotFound), "Contact not found"},
		{"null both", 0, ""},
		{"unknown status no validation", HTTPStatus(999), "  spaced  "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewErrorMessage(tt.status, tt.message)
			if m.GetStatus() != tt.status {
				t.Errorf("status = %d, want %d", m.GetStatus(), tt.status)
			}
			if m.GetMessage() != tt.message {
				t.Errorf("message = %q, want %q", m.GetMessage(), tt.message)
			}
		})
	}
}

func TestStatusAccessors(t *testing.T) {
	tests := []struct {
		name string
		set  HTTPStatus
	}{
		{"bad request", HTTPStatus(http.StatusBadRequest)},
		{"null", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewErrorMessage(HTTPStatus(http.StatusNotFound), "x")
			m.SetStatus(tt.set)
			if got := m.GetStatus(); got != tt.set {
				t.Errorf("got %d, want %d", got, tt.set)
			}
			if m.GetMessage() != "x" {
				t.Errorf("message mutated")
			}
		})
	}
}

func TestMessageAccessors(t *testing.T) {
	tests := []struct {
		name string
		set  string
	}{
		{"invalid input", "Invalid input"},
		{"empty", ""},
		{"no trimming", "  padded  "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewErrorMessage(HTTPStatus(http.StatusNotFound), "orig")
			m.SetMessage(tt.set)
			if got := m.GetMessage(); got != tt.set {
				t.Errorf("got %q, want %q", got, tt.set)
			}
			if m.GetStatus() != HTTPStatus(http.StatusNotFound) {
				t.Errorf("status mutated")
			}
		})
	}
}

func TestEqual(t *testing.T) {
	nf := HTTPStatus(http.StatusNotFound)
	br := HTTPStatus(http.StatusBadRequest)
	tests := []struct {
		name string
		a, b *ErrorMessage
		want bool
	}{
		{"same values", NewErrorMessage(nf, "x"), NewErrorMessage(nf, "x"), true},
		{"both null fields", NewEmptyErrorMessage(), NewEmptyErrorMessage(), true},
		{"diff status", NewErrorMessage(nf, "x"), NewErrorMessage(br, "x"), false},
		{"diff message", NewErrorMessage(nf, "x"), NewErrorMessage(nf, "y"), false},
		{"other nil", NewErrorMessage(nf, "x"), nil, false},
		{"receiver nil", nil, NewErrorMessage(nf, "x"), false},
		{"both nil", nil, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.Equal(tt.b); got != tt.want {
				t.Errorf("a.Equal(b) = %v, want %v", got, tt.want)
			}
			if got := tt.b.Equal(tt.a); got != tt.want {
				t.Errorf("symmetry: b.Equal(a) = %v, want %v", got, tt.want)
			}
		})
	}

	a, b, c := NewErrorMessage(nf, "x"), NewErrorMessage(nf, "x"), NewErrorMessage(nf, "x")
	if !a.Equal(a) {
		t.Error("not reflexive")
	}
	if a.Equal(b) && b.Equal(c) && !a.Equal(c) {
		t.Error("not transitive")
	}
	// Value equality as a map key mirrors equal hashCodes.
	set := map[ErrorMessage]bool{*a: true}
	if !set[*b] {
		t.Error("equal values should hash equally")
	}
}

func TestErrorMessageString(t *testing.T) {
	tests := []struct {
		name string
		m    *ErrorMessage
		want string
	}{
		{"not found", NewErrorMessage(HTTPStatus(http.StatusNotFound), "x"), "ErrorMessage(status=404 NOT_FOUND, message=x)"},
		{"both null", NewEmptyErrorMessage(), "ErrorMessage(status=null, message=null)"},
		{"nil receiver", nil, "null"},
		{"teapot", NewErrorMessage(HTTPStatus(http.StatusTeapot), "t"), "ErrorMessage(status=418 I_AM_A_TEAPOT, message=t)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHTTPStatusName(t *testing.T) {
	tests := []struct {
		code int
		want string
	}{
		{0, ""},
		{999, ""},
		{http.StatusOK, "OK"},
		{http.StatusNotFound, "NOT_FOUND"},
		{http.StatusBadRequest, "BAD_REQUEST"},
		{http.StatusInternalServerError, "INTERNAL_SERVER_ERROR"},
		{http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE"},
		{http.StatusRequestURITooLong, "URI_TOO_LONG"},
		{http.StatusTeapot, "I_AM_A_TEAPOT"},
		{http.StatusUnprocessableEntity, "UNPROCESSABLE_ENTITY"},
		{http.StatusNonAuthoritativeInfo, "NON_AUTHORITATIVE_INFORMATION"},
		{http.StatusMultiStatus, "MULTI_STATUS"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			s := HTTPStatus(tt.code)
			if got := s.Name(); got != tt.want {
				t.Errorf("Name(%d) = %q, want %q", tt.code, got, tt.want)
			}
			if s.Code() != tt.code {
				t.Errorf("Code() = %d", s.Code())
			}
		})
	}
	if HTTPStatus(0).String() != "null" {
		t.Error("zero String should be null")
	}
	if HTTPStatus(404).String() != "404 NOT_FOUND" {
		t.Errorf("got %q", HTTPStatus(404).String())
	}
}

func TestParseHTTPStatus(t *testing.T) {
	tests := []struct {
		name    string
		want    HTTPStatus
		wantErr bool
	}{
		{"NOT_FOUND", 404, false},
		{"BAD_REQUEST", 400, false},
		{"I_AM_A_TEAPOT", 418, false},
		{"BOGUS", 0, true},
		{"", 0, true},
		{"not_found", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseHTTPStatus(tt.name)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestErrorMessageMarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		m       ErrorMessage
		want    string
		wantErr bool
	}{
		{"not found", ErrorMessage{Status: 404, Message: "User are not available"}, `{"status":"NOT_FOUND","message":"User are not available"}`, false},
		{"null status", ErrorMessage{}, `{"status":null,"message":""}`, false},
		{"unknown status", ErrorMessage{Status: 999, Message: "x"}, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := json.Marshal(tt.m)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && string(b) != tt.want {
				t.Errorf("got %s, want %s", b, tt.want)
			}
		})
	}
}

func TestErrorMessageUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    ErrorMessage
		wantErr bool
	}{
		{"full", `{"status":"BAD_REQUEST","message":"Invalid input"}`, ErrorMessage{Status: 400, Message: "Invalid input"}, false},
		{"null status", `{"status":null,"message":"m"}`, ErrorMessage{Message: "m"}, false},
		{"missing fields", `{}`, ErrorMessage{}, false},
		{"unknown name", `{"status":"NOPE"}`, ErrorMessage{}, true},
		{"numeric status", `{"status":404}`, ErrorMessage{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got ErrorMessage
			err := json.Unmarshal([]byte(tt.in), &got)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestErrorMessageHTTPRoundTrip(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		em := NewErrorMessage(HTTPStatus(http.StatusNotFound), "User are not available")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(em.GetStatus().Code())
		_ = json.NewEncoder(w).Encode(em)
	})
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d", resp.StatusCode)
	}
	var got ErrorMessage
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !got.Equal(NewErrorMessage(404, "User are not available")) {
		t.Errorf("got %v", got.String())
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(rec.Body.String(), `"status":"NOT_FOUND"`) {
		t.Errorf("body = %s", rec.Body.String())
	}
}