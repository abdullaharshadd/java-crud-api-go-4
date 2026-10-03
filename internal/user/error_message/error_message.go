package user

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// HTTPStatus is an HTTP status code that serialises to and from JSON the way
// Jackson handles Spring's org.springframework.http.HttpStatus enum: as the
// enum constant name, e.g. "NOT_FOUND". The zero value means "unset" (Java
// null) and is encoded as JSON null.
type HTTPStatus int

// springNameOverrides lists the codes whose Spring enum name cannot be derived
// mechanically from net/http's reason phrase.
var springNameOverrides = map[HTTPStatus]string{
	http.StatusRequestEntityTooLarge: "PAYLOAD_TOO_LARGE",
	http.StatusRequestURITooLong:     "URI_TOO_LONG",
	http.StatusTeapot:                "I_AM_A_TEAPOT",
	http.StatusMisdirectedRequest:    "MISDIRECTED_REQUEST",
	http.StatusUnprocessableEntity:   "UNPROCESSABLE_ENTITY",
	http.StatusNonAuthoritativeInfo:  "NON_AUTHORITATIVE_INFORMATION",
}

// Code returns the numeric HTTP status code.
func (s HTTPStatus) Code() int { return int(s) }

// Name returns the Spring HttpStatus enum constant name for s, for example
// "NOT_FOUND". It returns an empty string for the zero value or an unknown
// code.
func (s HTTPStatus) Name() string {
	if name, ok := springNameOverrides[s]; ok {
		return name
	}
	text := http.StatusText(int(s))
	if text == "" {
		return ""
	}
	text = strings.ToUpper(text)
	return strings.NewReplacer(" ", "_", "-", "_", "'", "").Replace(text)
}

// String matches Spring's HttpStatus.toString(): "<code> <NAME>",
// e.g. "404 NOT_FOUND". The zero value prints as "null".
func (s HTTPStatus) String() string {
	if s == 0 {
		return "null"
	}
	return fmt.Sprintf("%d %s", int(s), s.Name())
}

// ParseHTTPStatus resolves a Spring HttpStatus enum name (e.g. "NOT_FOUND")
// to its HTTPStatus value.
func ParseHTTPStatus(name string) (HTTPStatus, error) {
	for code := 100; code <= 599; code++ {
		s := HTTPStatus(code)
		if n := s.Name(); n != "" && n == name {
			return s, nil
		}
	}
	return 0, fmt.Errorf("unknown HTTP status name %q", name)
}

// MarshalJSON encodes s as its enum name, or null when unset.
func (s HTTPStatus) MarshalJSON() ([]byte, error) {
	if s == 0 {
		return []byte("null"), nil
	}
	name := s.Name()
	if name == "" {
		return nil, fmt.Errorf("cannot marshal unknown HTTP status %d", int(s))
	}
	return json.Marshal(name)
}

// UnmarshalJSON decodes an enum name (or null) into s.
func (s *HTTPStatus) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		*s = 0
		return nil
	}
	var name string
	if err := json.Unmarshal(data, &name); err != nil {
		return fmt.Errorf("decode HTTP status: %w", err)
	}
	parsed, err := ParseHTTPStatus(name)
	if err != nil {
		return err
	}
	*s = parsed
	return nil
}

// ErrorMessage is the JSON body returned to clients for handled errors,
// e.g. {"status":"NOT_FOUND","message":"User are not available"}.
// It replaces com.smartContact.model.ErrorMessage.
type ErrorMessage struct {
	Status  HTTPStatus `json:"status"`
	Message string     `json:"message"`
}

// NewEmptyErrorMessage returns an ErrorMessage with every field unset
// (Lombok @NoArgsConstructor).
func NewEmptyErrorMessage() *ErrorMessage {
	return &ErrorMessage{}
}

// NewErrorMessage returns an ErrorMessage initialised with status and
// message, in that order (Lombok @AllArgsConstructor).
func NewErrorMessage(status HTTPStatus, message string) *ErrorMessage {
	return &ErrorMessage{Status: status, Message: message}
}

// GetStatus returns the HTTP status.
func (m *ErrorMessage) GetStatus() HTTPStatus { return m.Status }

// SetStatus sets the HTTP status.
func (m *ErrorMessage) SetStatus(status HTTPStatus) { m.Status = status }

// GetMessage returns the human-readable message.
func (m *ErrorMessage) GetMessage() string { return m.Message }

// SetMessage sets the human-readable message.
func (m *ErrorMessage) SetMessage(message string) { m.Message = message }

// Equal reports value equality on status and message (Lombok @Data equals).
// Two nil pointers are equal; a nil and a non-nil pointer are not.
func (m *ErrorMessage) Equal(other *ErrorMessage) bool {
	if m == nil || other == nil {
		return m == other
	}
	return m.Status == other.Status && m.Message == other.Message
}

// String mirrors Lombok's toString, e.g.
// "ErrorMessage(status=404 NOT_FOUND, message=User are not available)".
//
// MIGRATION_NOTE: Go strings cannot be nil, so an empty message and an unset
// (Java null) message both print as "null".
func (m *ErrorMessage) String() string {
	if m == nil {
		return "null"
	}
	msg := m.Message
	if msg == "" {
		msg = "null"
	}
	return fmt.Sprintf("ErrorMessage(status=%s, message=%s)", m.Status, msg)
}
