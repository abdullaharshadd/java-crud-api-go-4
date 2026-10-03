// Package user contains the User domain model, its validation rules and the
// DDL that replaces Hibernate's ddl-auto schema generation for it.
package user

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrValidation is returned by Validate when a User violates a constraint.
var ErrValidation = errors.New("validation failed")

// NameRequiredMessage is the @NotBlank message from the source model. It is
// intended for logging only.
const NameRequiredMessage = "please Add the department Name"

// Physical table/column names (Hibernate SpringPhysicalNamingStrategy lower-cases
// the explicit @Column names).
const (
	TableName      = "user"
	ColumnID       = "user_id"
	ColumnName     = "user_name"
	ColumnEmail    = "user_email"
	ColumnPassword = "user_password"
	ColumnRole     = "user_role"
	ColumnAbout    = "user_about"
)

// User is the user domain model.
//
// MIGRATION_NOTE: Password is serialized to JSON for parity with the Java
// entity (Lombok/Jackson exposed it). This is a security issue — review.
type User struct {
	ID       int     `json:"id"`
	Name     *string `json:"name"`
	Email    *string `json:"email"`
	Password *string `json:"password"`
	Role     *string `json:"role"`
	About    *string `json:"about"`
}

// New returns an empty User where every field holds its zero value.
func New() *User { return &User{} }

// NewUser returns a User with every field set, in declaration order.
func NewUser(id int, name, email, password, role, about *string) *User {
	return &User{ID: id, Name: name, Email: email, Password: password, Role: role, About: about}
}

// GetID returns the user id.
func (u *User) GetID() int { return u.ID }

// SetID sets the user id.
func (u *User) SetID(id int) { u.ID = id }

// GetName returns the user name (nil when unset).
func (u *User) GetName() *string { return u.Name }

// SetName sets the user name.
func (u *User) SetName(v *string) { u.Name = v }

// GetEmail returns the user email (nil when unset).
func (u *User) GetEmail() *string { return u.Email }

// SetEmail sets the user email.
func (u *User) SetEmail(v *string) { u.Email = v }

// GetPassword returns the user password (nil when unset).
func (u *User) GetPassword() *string { return u.Password }

// SetPassword sets the user password.
func (u *User) SetPassword(v *string) { u.Password = v }

// GetRole returns the user role (nil when unset).
func (u *User) GetRole() *string { return u.Role }

// SetRole sets the user role.
func (u *User) SetRole(v *string) { u.Role = v }

// GetAbout returns the user about text (nil when unset).
func (u *User) GetAbout() *string { return u.About }

// SetAbout sets the user about text.
func (u *User) SetAbout(v *string) { u.About = v }

// Validate enforces the @NotBlank constraint on Name: it must be non-nil and
// contain at least one character greater than ' ' (Java String.trim semantics).
func (u *User) Validate() error {
	if u.Name == nil || javaTrim(*u.Name) == "" {
		return fmt.Errorf("%w: name: %s", ErrValidation, NameRequiredMessage)
	}
	return nil
}

func javaTrim(s string) string {
	return strings.TrimFunc(s, func(r rune) bool { return r <= ' ' })
}

func eqPtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// Equal reports value equality over all six fields.
func (u *User) Equal(o *User) bool {
	if u == nil || o == nil {
		return u == o
	}
	return u.ID == o.ID &&
		eqPtr(u.Name, o.Name) &&
		eqPtr(u.Email, o.Email) &&
		eqPtr(u.Password, o.Password) &&
		eqPtr(u.Role, o.Role) &&
		eqPtr(u.About, o.About)
}

func strOrNull(p *string) string {
	if p == nil {
		return "null"
	}
	return *p
}

// String returns a Lombok-style representation containing every field.
func (u *User) String() string {
	if u == nil {
		return "null"
	}
	return "User(id=" + strconv.Itoa(u.ID) +
		", name=" + strOrNull(u.Name) +
		", email=" + strOrNull(u.Email) +
		", password=" + strOrNull(u.Password) +
		", role=" + strOrNull(u.Role) +
		", about=" + strOrNull(u.About) + ")"
}

// Builder is a fluent builder for User.
type Builder struct{ u User }

// NewBuilder returns a new User builder.
func NewBuilder() *Builder { return &Builder{} }

// ID sets the id.
func (b *Builder) ID(v int) *Builder { b.u.ID = v; return b }

// Name sets the name.
func (b *Builder) Name(v string) *Builder { b.u.Name = &v; return b }

// Email sets the email.
func (b *Builder) Email(v string) *Builder { b.u.Email = &v; return b }

// Password sets the password.
func (b *Builder) Password(v string) *Builder { b.u.Password = &v; return b }

// Role sets the role.
func (b *Builder) Role(v string) *Builder { b.u.Role = &v; return b }

// About sets the about text.
func (b *Builder) About(v string) *Builder { b.u.About = &v; return b }

// Build returns a new User with the configured values.
func (b *Builder) Build() *User {
	u := b.u
	return &u
}

// schemaStatements replaces Hibernate ddl-auto=update for the User entity on
// MySQL. hibernate_sequence mirrors GenerationType.AUTO on Hibernate 5/MySQL;
// user_id is also AUTO_INCREMENT so inserts without an explicit id still work.
var schemaStatements = []string{
	"CREATE TABLE IF NOT EXISTS `user` (" +
		"user_id INT NOT NULL AUTO_INCREMENT, " +
		"user_name VARCHAR(255) NULL, " +
		"user_email VARCHAR(255) NULL, " +
		"user_password VARCHAR(255) NULL, " +
		"user_role VARCHAR(255) NULL, " +
		"user_about VARCHAR(500) NULL, " +
		"PRIMARY KEY (user_id), " +
		"CONSTRAINT uk_user_email UNIQUE (user_email)" +
		") ENGINE=InnoDB",
	"CREATE TABLE IF NOT EXISTS hibernate_sequence (next_val BIGINT) ENGINE=InnoDB",
	"INSERT INTO hibernate_sequence (next_val) SELECT 1 FROM DUAL " +
		"WHERE NOT EXISTS (SELECT 1 FROM hibernate_sequence)",
}

// Execer is the subset of *sql.DB / *sql.Tx needed to apply the schema.
type Execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// EnsureSchema creates the user table and id sequence if they do not exist.
// Call it once at application startup.
func EnsureSchema(ctx context.Context, db Execer) error {
	for _, stmt := range schemaStatements {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("user: ensure schema: %w", err)
		}
	}
	return nil
}
