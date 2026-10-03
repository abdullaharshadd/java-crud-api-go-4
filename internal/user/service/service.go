package user

import (
	"context"
	"errors"
	"fmt"

	"migrated-app/internal/user/repository"
)

// UserService is the business-layer contract for user operations. It
// replaces com.smartContact.service.UserService and its implementation
// UserServiceImp. HTTP handlers depend on this interface, not on the
// concrete implementation.
//
// MIGRATION_NOTE: the entity type is repository.User because model.go in
// this directory declares `package model` while errors.go (and this file)
// declare `package user`. Until that clash is fixed, this package cannot
// import its own model. Once it is fixed, alias repository.User to the
// single domain User type.
type UserService interface {
	// SaveUser persists u and returns the saved entity with its id set.
	SaveUser(ctx context.Context, u *repository.User) (*repository.User, error)
	// FetchUserList returns every stored user. The slice is never nil.
	FetchUserList(ctx context.Context) ([]*repository.User, error)
	// FetchUserByID returns the user with the given id. If there is none,
	// the error is a *NotFoundError that matches ErrUserNotFound.
	FetchUserByID(ctx context.Context, id int) (*repository.User, error)
	// DeleteUser removes the user with the given id.
	DeleteUser(ctx context.Context, id int) error
	// UpdateUser sets u's id to id and saves it. With JPA merge semantics,
	// the row is overwritten if it exists. Otherwise a new row is created.
	UpdateUser(ctx context.Context, id int, u *repository.User) error
	// GetUserNameByName looks up a user by exact name. The bool is false
	// when no user matches, which was a null return in Java.
	GetUserNameByName(ctx context.Context, name string) (*repository.User, bool, error)
}

// userService is the default UserService, backed by a repository.UserDao.
type userService struct {
	dao repository.UserDao
}

// NewService returns a UserService that uses dao for persistence. It
// replaces Spring's @Service and @Autowired field injection. Call it from
// cmd/server/main.go after building the repository.
func NewService(dao repository.UserDao) UserService {
	return &userService{dao: dao}
}

// errNilUser is reported when a nil user is passed to a write operation.
var errNilUser = errors.New("user must not be nil")

// SaveUser implements UserService.
func (s *userService) SaveUser(ctx context.Context, u *repository.User) (*repository.User, error) {
	if u == nil {
		return nil, fmt.Errorf("save user: %w: %w", ErrValidation, errNilUser)
	}
	saved, err := s.dao.Save(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("save user: %w", err)
	}
	return saved, nil
}

// FetchUserList implements UserService.
func (s *userService) FetchUserList(ctx context.Context) ([]*repository.User, error) {
	users, err := s.dao.FindAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch user list: %w", err)
	}
	if users == nil {
		users = make([]*repository.User, 0)
	}
	return users, nil
}

// FetchUserByID implements UserService.
func (s *userService) FetchUserByID(ctx context.Context, id int) (*repository.User, error) {
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

// DeleteUser implements UserService.
func (s *userService) DeleteUser(ctx context.Context, id int) error {
	if err := s.dao.DeleteByID(ctx, id); err != nil {
		if errors.Is(err, repository.ErrNothingDeleted) {
			// Spring Data's deleteById threw EmptyResultDataAccessException.
			// Translate the repository sentinel to the package-level one.
			return fmt.Errorf("delete user %d: %w: %w", id, ErrNothingDeleted, err)
		}
		return fmt.Errorf("delete user %d: %w", id, err)
	}
	return nil
}

// UpdateUser implements UserService.
func (s *userService) UpdateUser(ctx context.Context, id int, u *repository.User) error {
	if u == nil {
		return fmt.Errorf("update user %d: %w: %w", id, ErrValidation, errNilUser)
	}
	u.ID = id
	if _, err := s.dao.Save(ctx, u); err != nil {
		return fmt.Errorf("update user %d: %w", id, err)
	}
	return nil
}

// GetUserNameByName implements UserService.
func (s *userService) GetUserNameByName(ctx context.Context, name string) (*repository.User, bool, error) {
	u, ok, err := s.dao.FindByName(ctx, name)
	if err != nil {
		if errors.Is(err, repository.ErrNotUnique) {
			return nil, false, fmt.Errorf("get user by name: %w: %w", ErrNotUnique, err)
		}
		return nil, false, fmt.Errorf("get user by name: %w", err)
	}
	if !ok {
		return nil, false, nil
	}
	return u, true, nil
}
