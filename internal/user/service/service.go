package user

import (
	"context"
	"errors"
	"fmt"

	"migrated-app/internal/user/repository"
)

// Service is the business-layer contract for user operations. It replaces
// com.smartContact.service.UserService and its implementation
// UserServiceImp. HTTP handlers depend on this interface.
//
// MIGRATION_NOTE: the User type here is repository.User. model.go in this
// directory declares `package model`, while errors.go and this file declare
// `package user`. Once that clash is fixed and model.User is the single
// domain type, switch this to that type (or alias repository.User to it).
type Service interface {
	// SaveUser persists u and returns the saved entity with its generated
	// id filled in.
	SaveUser(ctx context.Context, u *repository.User) (*repository.User, error)
	// FetchUserList returns every stored user. The slice is never nil.
	FetchUserList(ctx context.Context) ([]*repository.User, error)
	// FetchUserByID returns the user with the given id. If there is none,
	// the error matches ErrUserNotFound and is a *NotFoundError.
	FetchUserByID(ctx context.Context, id int) (*repository.User, error)
	// DeleteUser removes the user with the given id. If no row matched,
	// the error wraps ErrNothingDeleted.
	DeleteUser(ctx context.Context, id int) error
	// UpdateUser sets u's id to id and saves it, using JPA merge semantics.
	// If id does not exist, a new row with a generated id is inserted.
	UpdateUser(ctx context.Context, id int, u *repository.User) error
	// GetUserNameByName looks up a user by exact name. It returns
	// (nil, nil) when no user matches, mirroring the Java null return.
	GetUserNameByName(ctx context.Context, name string) (*repository.User, error)
}

// service is the default Service, backed by a repository.UserDao.
type service struct {
	dao repository.UserDao
}

// NewService returns a Service that uses dao for persistence. It replaces
// Spring's @Service and @Autowired wiring. Call it from cmd/server/main.go
// after the repository has been built.
func NewService(dao repository.UserDao) Service {
	return &service{dao: dao}
}

var errNilUser = errors.New("user must not be nil")

// SaveUser implements Service.
func (s *service) SaveUser(ctx context.Context, u *repository.User) (*repository.User, error) {
	if u == nil {
		return nil, fmt.Errorf("save user: %w: %w", ErrValidation, errNilUser)
	}
	saved, err := s.dao.Save(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("save user: %w", err)
	}
	return saved, nil
}

// FetchUserList implements Service.
func (s *service) FetchUserList(ctx context.Context) ([]*repository.User, error) {
	users, err := s.dao.FindAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch user list: %w", err)
	}
	if users == nil {
		users = make([]*repository.User, 0)
	}
	return users, nil
}

// FetchUserByID implements Service.
func (s *service) FetchUserByID(ctx context.Context, id int) (*repository.User, error) {
	u, ok, err := s.dao.FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("fetch user %d: %w", id, err)
	}
	if !ok {
		// Same message as Java's new UserNotFoundException("User are not available").
		return nil, NewNotFoundError(UserNotFoundMessage, nil)
	}
	return u, nil
}

// DeleteUser implements Service.
func (s *service) DeleteUser(ctx context.Context, id int) error {
	if err := s.dao.DeleteByID(ctx, id); err != nil {
		if errors.Is(err, repository.ErrNothingDeleted) {
			// Map the repository sentinel onto the package-level one, so
			// handlers only need errors.Is(err, ErrNothingDeleted).
			return fmt.Errorf("delete user %d: %w: %w", id, ErrNothingDeleted, err)
		}
		return fmt.Errorf("delete user %d: %w", id, err)
	}
	return nil
}

// UpdateUser implements Service.
func (s *service) UpdateUser(ctx context.Context, id int, u *repository.User) error {
	if u == nil {
		return fmt.Errorf("update user %d: %w: %w", id, ErrValidation, errNilUser)
	}
	u.ID = id
	if _, err := s.dao.Save(ctx, u); err != nil {
		return fmt.Errorf("update user %d: %w", id, err)
	}
	return nil
}

// GetUserNameByName implements Service.
func (s *service) GetUserNameByName(ctx context.Context, name string) (*repository.User, error) {
	u, ok, err := s.dao.FindByName(ctx, name)
	if err != nil {
		if errors.Is(err, repository.ErrNotUnique) {
			return nil, fmt.Errorf("get user by name: %w: %w", ErrNotUnique, err)
		}
		return nil, fmt.Errorf("get user by name: %w", err)
	}
	if !ok {
		return nil, nil
	}
	return u, nil
}
