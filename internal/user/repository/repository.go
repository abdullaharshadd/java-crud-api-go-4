package user

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// MIGRATION_NOTE: this file replaces the Spring Data JPA interface
// com.smartContact.repository.UserDao (JpaRepository<User, Integer> plus the
// derived query findByName). Spring generates that implementation at runtime.
// Here it is written out by hand on top of database/sql for MySQL.
//
// MIGRATION_NOTE (manual review required): internal/user/model.go declares
// `package model` while internal/user/errors.go declares `package user`.
// Two packages in one directory cannot compile. This file uses `package user`,
// as the plan and errors.go do, and expects model.go to switch to
// `package user` as well. When it does, delete one of the two duplicate
// ErrValidation declarations.

// ErrInvalidSortProperty is returned when a Sort names a property that the
// User entity does not have. It mirrors Spring's PropertyReferenceException.
var ErrInvalidSortProperty = errors.New("no property found for type User")

// ErrInvalidPageRequest is returned for a negative page number or a page size
// below 1. It mirrors the IllegalArgumentException thrown by PageRequest.of.
var ErrInvalidPageRequest = errors.New("invalid page request")

// Direction is a sort direction.
type Direction int

const (
	// Asc sorts ascending.
	Asc Direction = iota
	// Desc sorts descending.
	Desc
)

// Order is one sort criterion. Property is a User field name (id, name,
// email, password, role, about).
type Order struct {
	Property  string
	Direction Direction
}

// Sort is an ordered list of sort criteria. It replaces Spring Data's Sort.
type Sort []Order

// PageRequest selects a zero-based page of results with an optional sort.
// It replaces Spring Data's Pageable.
type PageRequest struct {
	Page int
	Size int
	Sort Sort
}

// Page is one page of users plus paging metadata. It replaces Spring Data's
// Page<User>.
type Page struct {
	Content       []*User
	Number        int
	Size          int
	TotalElements int64
	TotalPages    int
}

// Repository is the persistence contract for users. It covers the
// JpaRepository operations the source inherited plus FindByName.
type Repository interface {
	// Save inserts u when u.ID is 0. Otherwise it updates the row with that
	// id, or inserts a new row with a generated id if no such row exists
	// (JPA merge semantics). It returns the saved user.
	Save(ctx context.Context, u *User) (*User, error)
	// SaveAll saves every user in a single transaction.
	SaveAll(ctx context.Context, users []*User) ([]*User, error)
	// FindByID returns the user with the given id. The bool is false when
	// no such user exists.
	FindByID(ctx context.Context, id int) (*User, bool, error)
	// ExistsByID reports whether a user with the given id exists.
	ExistsByID(ctx context.Context, id int) (bool, error)
	// FindAll returns every user. The result is never nil.
	FindAll(ctx context.Context) ([]*User, error)
	// FindAllSorted returns every user, ordered by s.
	FindAllSorted(ctx context.Context, s Sort) ([]*User, error)
	// FindAllPaged returns one page of users.
	FindAllPaged(ctx context.Context, p PageRequest) (Page, error)
	// FindAllByID returns the users whose ids are listed. Missing ids are
	// skipped.
	FindAllByID(ctx context.Context, ids []int) ([]*User, error)
	// Count returns the number of users.
	Count(ctx context.Context) (int64, error)
	// DeleteByID deletes the user with the given id. It returns
	// ErrNothingDeleted if no row matched.
	DeleteByID(ctx context.Context, id int) error
	// Delete deletes the given user. It does nothing if the user is new
	// (ID 0) or already gone.
	Delete(ctx context.Context, u *User) error
	// DeleteAllByID deletes the users with the given ids.
	DeleteAllByID(ctx context.Context, ids []int) error
	// DeleteAll deletes every user.
	DeleteAll(ctx context.Context) error
	// FindByName returns the user whose name exactly equals name. The bool
	// is false when there is none. It returns ErrNotUnique when more than
	// one user matches.
	FindByName(ctx context.Context, name string) (*User, bool, error)
}

// MySQLRepository implements Repository on MySQL.
type MySQLRepository struct {
	db *sql.DB
}

// NewMySQLRepository returns a Repository backed by db.
func NewMySQLRepository(db *sql.DB) *MySQLRepository {
	return &MySQLRepository{db: db}
}

var _ Repository = (*MySQLRepository)(nil)

const userColumns = "user_id, user_name, user_email, user_password, user_role, user_about"

const selectUsers = "SELECT " + userColumns + " FROM `user`"

// sortColumns maps User property names to their column names.
var sortColumns = map[string]string{
	"id":       "user_id",
	"name":     "user_name",
	"email":    "user_email",
	"password": "user_password",
	"role":     "user_role",
	"about":    "user_about",
}

type rowScanner interface {
	Scan(dest ...any) error
}

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func nullToPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	s := ns.String
	return &s
}

func ptrToNull(p *string) sql.NullString {
	if p == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *p, Valid: true}
}

func scanUser(s rowScanner) (*User, error) {
	var (
		u                                User
		name, email, pass, role, about sql.NullString
	)
	if err := s.Scan(&u.ID, &name, &email, &pass, &role, &about); err != nil {
		return nil, err
	}
	u.Name = nullToPtr(name)
	u.Email = nullToPtr(email)
	u.Password = nullToPtr(pass)
	u.Role = nullToPtr(role)
	u.About = nullToPtr(about)
	return &u, nil
}

func queryUsers(ctx context.Context, q queryer, query string, args ...any) ([]*User, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := make([]*User, 0)
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return users, nil
}

func orderByClause(s Sort) (string, error) {
	if len(s) == 0 {
		return "", nil
	}
	parts := make([]string, 0, len(s))
	for _, o := range s {
		col, ok := sortColumns[o.Property]
		if !ok {
			return "", fmt.Errorf("%w: %q", ErrInvalidSortProperty, o.Property)
		}
		dir := "ASC"
		if o.Direction == Desc {
			dir = "DESC"
		}
		parts = append(parts, col+" "+dir)
	}
	return " ORDER BY " + strings.Join(parts, ", "), nil
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}

func intsToArgs(ids []int) []any {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return args
}

// Save implements Repository.
func (r *MySQLRepository) Save(ctx context.Context, u *User) (*User, error) {
	if u == nil {
		return nil, errors.New("user repository: save: entity must not be nil")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("user repository: save: begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op

	saved, err := saveTx(ctx, tx, u)
	if err != nil {
		return nil, fmt.Errorf("user repository: save: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("user repository: save: commit: %w", err)
	}
	return saved, nil
}

// SaveAll implements Repository.
func (r *MySQLRepository) SaveAll(ctx context.Context, users []*User) ([]*User, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("user repository: save all: begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op

	out := make([]*User, 0, len(users))
	for _, u := range users {
		if u == nil {
			return nil, errors.New("user repository: save all: entity must not be nil")
		}
		saved, err := saveTx(ctx, tx, u)
		if err != nil {
			return nil, fmt.Errorf("user repository: save all: %w", err)
		}
		out = append(out, saved)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("user repository: save all: commit: %w", err)
	}
	return out, nil
}

// saveTx has JPA save semantics: persist when new (ID 0), otherwise merge.
// A merge whose id does not exist inserts a new row with a generated id,
// which is what Hibernate's merge does with a generated identifier.
func saveTx(ctx context.Context, tx *sql.Tx, u *User) (*User, error) {
	if u.ID != 0 {
		var existing int
		err := tx.QueryRowContext(ctx,
			"SELECT user_id FROM `user` WHERE user_id = ? FOR UPDATE", u.ID).Scan(&existing)
		switch {
		case err == nil:
			_, err = tx.ExecContext(ctx,
				"UPDATE `user` SET user_name = ?, user_email = ?, user_password = ?, user_role = ?, user_about = ? WHERE user_id = ?",
				ptrToNull(u.Name), ptrToNull(u.Email), ptrToNull(u.Password),
				ptrToNull(u.Role), ptrToNull(u.About), u.ID)
			if err != nil {
				return nil, fmt.Errorf("update id %d: %w", u.ID, err)
			}
			merged := *u
			return &merged, nil
		case errors.Is(err, sql.ErrNoRows):
			// The id was supplied but does not exist: merge inserts a new
			// row with a generated id and returns a copy.
			merged := *u
			if err := insertTx(ctx, tx, &merged); err != nil {
				return nil, err
			}
			return &merged, nil
		default:
			return nil, fmt.Errorf("lock id %d: %w", u.ID, err)
		}
	}
	// New entity (persist). The id is assigned to the passed instance, as
	// JPA persist does.
	if err := insertTx(ctx, tx, u); err != nil {
		return nil, err
	}
	return u, nil
}

func insertTx(ctx context.Context, tx *sql.Tx, u *User) error {
	res, err := tx.ExecContext(ctx,
		"INSERT INTO `user` (user_name, user_email, user_password, user_role, user_about) VALUES (?, ?, ?, ?, ?)",
		ptrToNull(u.Name), ptrToNull(u.Email), ptrToNull(u.Password),
		ptrToNull(u.Role), ptrToNull(u.About))
	if err != nil {
		return fmt.Errorf("insert: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("insert: last insert id: %w", err)
	}
	u.ID = int(id)
	return nil
}

// FindByID implements Repository.
func (r *MySQLRepository) FindByID(ctx context.Context, id int) (*User, bool, error) {
	u, err := scanUser(r.db.QueryRowContext(ctx, selectUsers+" WHERE user_id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("user repository: find by id %d: %w", id, err)
	}
	return u, true, nil
}

// ExistsByID implements Repository.
func (r *MySQLRepository) ExistsByID(ctx context.Context, id int) (bool, error) {
	var n int64
	if err := r.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM `user` WHERE user_id = ?", id).Scan(&n); err != nil {
		return false, fmt.Errorf("user repository: exists by id %d: %w", id, err)
	}
	return n > 0, nil
}

// FindAll implements Repository.
func (r *MySQLRepository) FindAll(ctx context.Context) ([]*User, error) {
	users, err := queryUsers(ctx, r.db, selectUsers)
	if err != nil {
		return nil, fmt.Errorf("user repository: find all: %w", err)
	}
	return users, nil
}

// FindAllSorted implements Repository.
func (r *MySQLRepository) FindAllSorted(ctx context.Context, s Sort) ([]*User, error) {
	order, err := orderByClause(s)
	if err != nil {
		return nil, fmt.Errorf("user repository: find all sorted: %w", err)
	}
	users, err := queryUsers(ctx, r.db, selectUsers+order)
	if err != nil {
		return nil, fmt.Errorf("user repository: find all sorted: %w", err)
	}
	return users, nil
}

// FindAllPaged implements Repository.
func (r *MySQLRepository) FindAllPaged(ctx context.Context, p PageRequest) (Page, error) {
	if p.Page < 0 || p.Size < 1 {
		return Page{}, fmt.Errorf("user repository: find all paged: %w: page=%d size=%d",
			ErrInvalidPageRequest, p.Page, p.Size)
	}
	order, err := orderByClause(p.Sort)
	if err != nil {
		return Page{}, fmt.Errorf("user repository: find all paged: %w", err)
	}
	total, err := r.Count(ctx)
	if err != nil {
		return Page{}, fmt.Errorf("user repository: find all paged: %w", err)
	}
	offset := int64(p.Page) * int64(p.Size)
	users, err := queryUsers(ctx, r.db, selectUsers+order+" LIMIT ? OFFSET ?", p.Size, offset)
	if err != nil {
		return Page{}, fmt.Errorf("user repository: find all paged: %w", err)
	}
	pages := int((total + int64(p.Size) - 1) / int64(p.Size))
	return Page{
		Content:       users,
		Number:        p.Page,
		Size:          p.Size,
		TotalElements: total,
		TotalPages:    pages,
	}, nil
}

// FindAllByID implements Repository.
func (r *MySQLRepository) FindAllByID(ctx context.Context, ids []int) ([]*User, error) {
	if len(ids) == 0 {
		return make([]*User, 0), nil
	}
	users, err := queryUsers(ctx, r.db,
		selectUsers+" WHERE user_id IN ("+placeholders(len(ids))+")", intsToArgs(ids)...)
	if err != nil {
		return nil, fmt.Errorf("user repository: find all by id: %w", err)
	}
	return users, nil
}

// Count implements Repository.
func (r *MySQLRepository) Count(ctx context.Context) (int64, error) {
	var n int64
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM `user`").Scan(&n); err != nil {
		return 0, fmt.Errorf("user repository: count: %w", err)
	}
	return n, nil
}

// DeleteByID implements Repository.
func (r *MySQLRepository) DeleteByID(ctx context.Context, id int) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM `user` WHERE user_id = ?", id)
	if err != nil {
		return fmt.Errorf("user repository: delete by id %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("user repository: delete by id %d: rows affected: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("user repository: delete by id %d: %w", id, ErrNothingDeleted)
	}
	return nil
}

// Delete implements Repository.
func (r *MySQLRepository) Delete(ctx context.Context, u *User) error {
	if u == nil {
		return errors.New("user repository: delete: entity must not be nil")
	}
	if u.ID == 0 {
		return nil // new entity: nothing to delete, as in SimpleJpaRepository
	}
	if _, err := r.db.ExecContext(ctx, "DELETE FROM `user` WHERE user_id = ?", u.ID); err != nil {
		return fmt.Errorf("user repository: delete id %d: %w", u.ID, err)
	}
	return nil
}

// DeleteAllByID implements Repository.
func (r *MySQLRepository) DeleteAllByID(ctx context.Context, ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	if _, err := r.db.ExecContext(ctx,
		"DELETE FROM `user` WHERE user_id IN ("+placeholders(len(ids))+")", intsToArgs(ids)...); err != nil {
		return fmt.Errorf("user repository: delete all by id: %w", err)
	}
	return nil
}

// DeleteAll implements Repository.
func (r *MySQLRepository) DeleteAll(ctx context.Context) error {
	if _, err := r.db.ExecContext(ctx, "DELETE FROM `user`"); err != nil {
		return fmt.Errorf("user repository: delete all: %w", err)
	}
	return nil
}

// FindByName implements Repository. It replaces the derived query
// UserDao.findByName (WHERE user_name = ?). Spring Data throws
// IncorrectResultSizeDataAccessException when more than one row matches;
// this returns ErrNotUnique instead.
func (r *MySQLRepository) FindByName(ctx context.Context, name string) (*User, bool, error) {
	users, err := queryUsers(ctx, r.db, selectUsers+" WHERE user_name = ? LIMIT 2", name)
	if err != nil {
		return nil, false, fmt.Errorf("user repository: find by name: %w", err)
	}
	switch len(users) {
	case 0:
		return nil, false, nil
	case 1:
		return users[0], true, nil
	default:
		return nil, false, fmt.Errorf("user repository: find by name %q: %w", name, ErrNotUnique)
	}
}
