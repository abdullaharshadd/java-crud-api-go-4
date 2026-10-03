// Package handler exposes the user CRUD endpoints over HTTP. It replaces
// com.smartContact.Controller.UserController (a Spring @RestController).
//
// MIGRATION_NOTE: the agreed path was internal/user/handler.go. That
// directory currently cannot compile (model.go declares `package model`
// while errors.go declares `package user`), so this file lives in its own
// package. It also declares its own narrow Service interface instead of
// importing internal/user/service, which currently references symbols from
// another directory. The concrete service returned by service.NewService
// satisfies Service structurally, so wiring needs no adapter.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"

	"migrated-app/internal/httpx"
	"migrated-app/internal/user/repository"
)

// maxBodyBytes bounds request bodies read by the JSON endpoints.
const maxBodyBytes = 10 << 20

// Response texts, preserved verbatim from the Java controller.
const (
	msgSaved   = "User data saved successfully!"
	msgDeleted = "user data deleted Successfully"
)

// Service is the subset of the user business layer the handler needs. It
// mirrors com.smartContact.service.UserService.
type Service interface {
	// SaveUser persists u.
	SaveUser(ctx context.Context, u *repository.User) (*repository.User, error)
	// FetchUserList returns all users; never nil.
	FetchUserList(ctx context.Context) ([]*repository.User, error)
	// FetchUserByID returns the user with id or a not-found error.
	FetchUserByID(ctx context.Context, id int) (*repository.User, error)
	// DeleteUser removes the user with id.
	DeleteUser(ctx context.Context, id int) error
	// UpdateUser sets u's id to id and saves it.
	UpdateUser(ctx context.Context, id int, u *repository.User) error
	// GetUserNameByName looks up a user by name; false when none matches.
	GetUserNameByName(ctx context.Context, name string) (*repository.User, bool, error)
}

// Handler serves the user endpoints.
type Handler struct {
	svc  Service
	errs *httpx.ErrorHandler
}

// New returns a Handler backed by svc. errs maps returned errors to
// responses; build it with httpx.NewErrorHandler(user.ErrUserNotFound) so
// missing users produce the 404 ErrorMessage body.
func New(svc Service, errs *httpx.ErrorHandler) (*Handler, error) {
	if svc == nil {
		return nil, errors.New("handler: nil Service")
	}
	if errs == nil {
		return nil, errors.New("handler: nil ErrorHandler")
	}
	return &Handler{svc: svc, errs: errs}, nil
}

// Register adds every user route to mux, each with and without a trailing
// slash (Spring MVC's default trailing-slash matching).
func (h *Handler) Register(mux *http.ServeMux) {
	routes := []struct {
		method string
		paths  []string
		fn     httpx.HandlerFunc
	}{
		{http.MethodPost, []string{"/save_user_data", "/save_user_data/{$}"}, h.saveUser},
		{http.MethodGet, []string{"/get_user_data", "/get_user_data/{$}"}, h.fetchUserList},
		{http.MethodGet, []string{"/get_user_data/{id}", "/get_user_data/{id}/{$}"}, h.fetchUserByID},
		{http.MethodDelete, []string{"/delete_user_data/{id}", "/delete_user_data/{id}/{$}"}, h.deleteUser},
		{http.MethodPut, []string{"/update_user_data/{id}", "/update_user_data/{id}/{$}"}, h.updateUser},
		{http.MethodGet, []string{"/get_user_name/name/{name}", "/get_user_name/name/{name}/{$}"}, h.getUserNameByName},
	}
	for _, rt := range routes {
		for _, p := range rt.paths {
			mux.Handle(rt.method+" "+p, h.errs.Handle(rt.fn))
		}
	}
}

// saveUser handles POST /save_user_data.
func (h *Handler) saveUser(w http.ResponseWriter, r *http.Request) error {
	log.Info().Msg("inside the saveUser of UserController")
	raw, err := readJSONBody(r)
	if err != nil {
		return err
	}
	u, err := decodeUser(raw)
	if err != nil {
		return err
	}
	if err := validateUser(raw); err != nil {
		// @Valid failure: MethodArgumentNotValidException -> 400 empty body.
		return httpx.NewBadRequestError(err)
	}
	if _, err := h.svc.SaveUser(r.Context(), u); err != nil {
		return fmt.Errorf("save user: %w", err)
	}
	return httpx.WriteText(w, http.StatusOK, msgSaved)
}

// fetchUserList handles GET /get_user_data.
func (h *Handler) fetchUserList(w http.ResponseWriter, r *http.Request) error {
	log.Info().Msg("inside the fetchUserList of UserController")
	users, err := h.svc.FetchUserList(r.Context())
	if err != nil {
		return fmt.Errorf("fetch user list: %w", err)
	}
	if users == nil {
		users = []*repository.User{}
	}
	return httpx.WriteJSON(w, http.StatusOK, users)
}

// fetchUserByID handles GET /get_user_data/{id}.
func (h *Handler) fetchUserByID(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	u, err := h.svc.FetchUserByID(r.Context(), id)
	if err != nil {
		// Not-found errors are matched by the ErrorHandler and become 404.
		return err
	}
	return httpx.WriteJSON(w, http.StatusOK, u)
}

// deleteUser handles DELETE /delete_user_data/{id}.
func (h *Handler) deleteUser(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	if err := h.svc.DeleteUser(r.Context(), id); err != nil {
		// Missing id surfaces as a 500 default error, as with Spring Data's
		// EmptyResultDataAccessException.
		return fmt.Errorf("delete user: %w", err)
	}
	return httpx.WriteText(w, http.StatusOK, msgDeleted)
}

// updateUser handles PUT /update_user_data/{id}. No validation is applied
// (the Java method had no @Valid). The request body is echoed back with its
// id set to the path id.
func (h *Handler) updateUser(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	raw, err := readJSONBody(r)
	if err != nil {
		return err
	}
	u, err := decodeUser(raw)
	if err != nil {
		return err
	}
	if err := h.svc.UpdateUser(r.Context(), id, u); err != nil {
		return fmt.Errorf("update user: %w", err)
	}
	u.ID = id
	return httpx.WriteJSON(w, http.StatusOK, u)
}

// getUserNameByName handles GET /get_user_name/name/{name}. No match yields
// 200 with no body and no Content-Type (Spring returning null).
func (h *Handler) getUserNameByName(w http.ResponseWriter, r *http.Request) error {
	name := r.PathValue("name")
	u, ok, err := h.svc.GetUserNameByName(r.Context(), name)
	if err != nil {
		return fmt.Errorf("get user by name: %w", err)
	}
	if !ok || u == nil {
		w.WriteHeader(http.StatusOK)
		return nil
	}
	return httpx.WriteJSON(w, http.StatusOK, u)
}

// pathID parses the {id} path variable as a Java int (signed 32-bit).
// Failure is a 400 with an empty body (MethodArgumentTypeMismatchException).
func pathID(r *http.Request) (int, error) {
	raw := r.PathValue("id")
	n, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		return 0, httpx.NewBadRequestError(fmt.Errorf("invalid id %q: %w", raw, err))
	}
	return int(n), nil
}

// readJSONBody enforces a JSON Content-Type (415 otherwise) and reads the
// body. An empty body is a 400 ("Required request body is missing").
func readJSONBody(r *http.Request) ([]byte, error) {
	if !isJSONContentType(r.Header.Get("Content-Type")) {
		return nil, httpx.NewUnsupportedMediaTypeError(
			fmt.Errorf("unsupported content type %q", r.Header.Get("Content-Type")),
			"application/json", "application/*+json")
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		return nil, httpx.NewBadRequestError(fmt.Errorf("read body: %w", err))
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, httpx.NewBadRequestError(errors.New("required request body is missing"))
	}
	return raw, nil
}

// isJSONContentType reports whether ct is application/json or a +json type.
// A missing Content-Type is treated as application/octet-stream, as Spring
// does, and is therefore rejected.
func isJSONContentType(ct string) bool {
	if ct == "" {
		return false
	}
	mt, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return false
	}
	mt = strings.ToLower(mt)
	return mt == "application/json" || (strings.HasPrefix(mt, "application/") && strings.HasSuffix(mt, "+json"))
}

// decodeUser unmarshals raw into a User. Unknown fields are ignored (Spring
// Boot's Jackson default). A JSON null body is rejected as missing.
func decodeUser(raw []byte) (*repository.User, error) {
	var u *repository.User
	if err := json.Unmarshal(raw, &u); err != nil {
		return nil, httpx.NewBadRequestError(fmt.Errorf("decode user: %w", err))
	}
	if u == nil {
		return nil, httpx.NewBadRequestError(errors.New("required request body is missing"))
	}
	return u, nil
}

// validationView captures the fields carrying Bean Validation constraints
// on the Java User entity.
type validationView struct {
	Name *string `json:"name"`
}

// nameRequiredMessage is the @NotBlank message from the Java model.
const nameRequiredMessage = "please Add the department Name"

// validateUser enforces @NotBlank on name (non-null and containing a
// character above ' ', matching Java's String.trim semantics).
func validateUser(raw []byte) error {
	var v validationView
	if err := json.Unmarshal(raw, &v); err != nil {
		return fmt.Errorf("validate user: %w", err)
	}
	if v.Name == nil || strings.TrimFunc(*v.Name, func(r rune) bool { return r <= ' ' }) == "" {
		return fmt.Errorf("name: %s", nameRequiredMessage)
	}
	return nil
}
