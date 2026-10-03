// Package model contains the smartContact domain models. This file holds the
// User entity, its validation rule, and the schema bootstrap that replaces
// Hibernate's spring.jpa.hibernate.ddl-auto=update for the user table.
//
// The schema is created automatically at application boot. The package init
// function connects to the configured MySQL database and runs
// CREATE TABLE IF NOT EXISTS for the user table. Any binary that imports this
// package, which every layer using the User model does, therefore gets the
// table without an extra step. EnsureSchema is also exported, so main or tests
// can apply the schema explicitly to a *sql.DB they already own.
package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	mysql "github.com/go-sql-driver/mysql"
)

// ErrValidation is returned by Validate when a User violates a constraint.
var ErrValidation = errors.New("validation failed")

// NameRequiredMessage is the @NotBlank message from the source model. The
// wording, including the reference to a department, is preserved verbatim.
const NameRequiredMessage = "please Add the department Name"

// Physical table and column names. Spring's default physical naming strategy
// lower-cases the explicit @Table and @Column names from the Java entity.
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
// entity, because Lombok and Jackson exposed it. This is a security issue and
// should be reviewed.
type User struct {
	ID       int     `json:"id"`
	Name     *string `json:"name"`
	Email    *string `json:"email"`
	Password *string `json:"password"`
	Role     *string `json:"role"`
	About    *string `json:"about"`
}

// New returns an empty User. It replaces Lombok's @NoArgsConstructor.
func New() *User { return &User{} }

// NewUser returns a User with every field set, in declaration order. It
// replaces Lombok's @AllArgsConstructor.
func NewUser(id int, name, email, password, role, about *string) *User {
	return &User{ID: id, Name: name, Email: email, Password: password, Role: role, About: about}
}

// GetID returns the user id.
func (u *User) GetID() int { return u.ID }

// SetID sets the user id.
func (u *User) SetID(id int) { u.ID = id }

// GetName returns the user name, or nil when unset.
func (u *User) GetName() *string { return u.Name }

// SetName sets the user name.
func (u *User) SetName(v *string) { u.Name = v }

// GetEmail returns the user email, or nil when unset.
func (u *User) GetEmail() *string { return u.Email }

// SetEmail sets the user email.
func (u *User) SetEmail(v *string) { u.Email = v }

// GetPassword returns the user password, or nil when unset.
func (u *User) GetPassword() *string { return u.Password }

// SetPassword sets the user password.
func (u *User) SetPassword(v *string) { u.Password = v }

// GetRole returns the user role, or nil when unset.
func (u *User) GetRole() *string { return u.Role }

// SetRole sets the user role.
func (u *User) SetRole(v *string) { u.Role = v }

// GetAbout returns the user about text, or nil when unset.
func (u *User) GetAbout() *string { return u.About }

// SetAbout sets the user about text.
func (u *User) SetAbout(v *string) { u.About = v }

// Validate enforces the @NotBlank constraint on Name. The name must be non-nil
// and contain at least one character greater than ' ', which matches Java's
// String.trim semantics.
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

// Equal reports value equality over all six fields. It replaces the
// Lombok @Data equals method.
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

// Builder is a fluent builder for User. It replaces Lombok's @Builder.
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

// ---------------------------------------------------------------------------
// Schema: replaces spring.jpa.hibernate.ddl-auto=update for the User entity.
// ---------------------------------------------------------------------------

// createUserTableSQL mirrors the Java entity exactly.
//   - @Id plus @GeneratedValue(AUTO) on int: user_id INT, auto-generated.
//   - name, password and role are String fields: VARCHAR(255), nullable.
//   - email is a String with unique = true: VARCHAR(255) plus a UNIQUE key.
//   - about is a String with length = 500: VARCHAR(500).
//
// MIGRATION_NOTE: Hibernate 5 with GenerationType.AUTO on MySQL uses a
// hibernate_sequence table. Here ids come from AUTO_INCREMENT instead, so no
// extra table is introduced. If rows were already written by the Java app,
// make sure AUTO_INCREMENT is above MAX(user_id). MySQL does this
// automatically for existing rows.
const createUserTableSQL = "CREATE TABLE IF NOT EXISTS `user` (" +
	"user_id INT NOT NULL AUTO_INCREMENT, " +
	"user_name VARCHAR(255) NULL, " +
	"user_email VARCHAR(255) NULL, " +
	"user_password VARCHAR(255) NULL, " +
	"user_role VARCHAR(255) NULL, " +
	"user_about VARCHAR(500) NULL, " +
	"PRIMARY KEY (user_id), " +
	"CONSTRAINT uk_user_email UNIQUE (user_email)" +
	") ENGINE=InnoDB"

// Execer is the subset of *sql.DB and *sql.Tx needed to apply the schema.
type Execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// EnsureSchema creates the user table if it does not exist. It is idempotent.
// It runs automatically at boot through this package's init function, and it
// may also be called explicitly against an existing connection.
func EnsureSchema(ctx context.Context, db Execer) error {
	if _, err := db.ExecContext(ctx, createUserTableSQL); err != nil {
		return fmt.Errorf("model: ensure user schema: %w", err)
	}
	return nil
}

// Environment variables consulted by the boot-time schema bootstrap.
const (
	// EnvDatabaseURL is the same variable internal/config loads as DatabaseURL.
	EnvDatabaseURL = "DATABASE_URL"
	// EnvSkipAutoSchema disables the boot-time bootstrap when set to a
	// non-empty value. This is useful for unit tests without a database.
	EnvSkipAutoSchema = "SMARTCONTACT_SKIP_AUTO_SCHEMA"
)

// Defaults taken from src/main/resources/application.properties.
const (
	defaultDBUser = "root"
	defaultDBPass = "root"
	defaultDBAddr = "localhost:3306"
	defaultDBName = "barcode"
)

const bootstrapTimeout = 15 * time.Second

// bootErr holds the result of the boot-time schema bootstrap.
var bootErr error

// SchemaBootstrapError returns the error, if any, from the automatic schema
// creation performed at boot. main should check it and fail fast when it is
// non-nil, just as Spring refuses to start when ddl-auto fails.
func SchemaBootstrapError() error { return bootErr }

// init creates the user table when the application boots, replacing
// Hibernate's ddl-auto=update. It never panics. Failures are logged and
// exposed through SchemaBootstrapError.
func init() {
	if os.Getenv(EnvSkipAutoSchema) != "" {
		return
	}
	bootErr = bootstrapSchema()
	if bootErr != nil {
		log.Printf("smartcontact/model: schema bootstrap failed: %v", bootErr)
	}
}

func bootstrapSchema() error {
	dsn, err := NormalizeDSN(os.Getenv(EnvDatabaseURL))
	if err != nil {
		return err
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("model: open database: %w", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), bootstrapTimeout)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("model: ping database: %w", err)
	}
	return EnsureSchema(ctx, db)
}

// NormalizeDSN converts a DATABASE_URL value into a go-sql-driver/mysql DSN.
// It accepts three forms:
//   - an empty string, which falls back to application.properties
//     (root:root@tcp(localhost:3306)/barcode);
//   - a URL such as mysql://user:pass@host:3306/db?k=v or jdbc:mysql://host/db;
//   - a native driver DSN such as user:pass@tcp(host:3306)/db, returned as is.
func NormalizeDSN(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	cfg := mysql.NewConfig()
	cfg.Net = "tcp"
	cfg.User = defaultDBUser
	cfg.Passwd = defaultDBPass
	cfg.Addr = defaultDBAddr
	cfg.DBName = defaultDBName

	if raw == "" {
		return cfg.FormatDSN(), nil
	}

	raw = strings.TrimPrefix(raw, "jdbc:")
	if !strings.HasPrefix(raw, "mysql://") {
		if _, err := mysql.ParseDSN(raw); err != nil {
			return "", fmt.Errorf("model: invalid %s: %w", EnvDatabaseURL, err)
		}
		return raw, nil
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("model: invalid %s: %w", EnvDatabaseURL, err)
	}
	if u.User != nil {
		cfg.User = u.User.Username()
		if pw, ok := u.User.Password(); ok {
			cfg.Passwd = pw
		}
	}
	if u.Host != "" {
		cfg.Addr = u.Host
		if _, _, splitErr := net.SplitHostPort(u.Host); splitErr != nil {
			cfg.Addr = net.JoinHostPort(u.Host, "3306")
		}
	}
	if name := strings.TrimPrefix(u.Path, "/"); name != "" {
		cfg.DBName = name
	}
	if q := u.Query(); len(q) > 0 {
		cfg.Params = make(map[string]string, len(q))
		for k := range q {
			cfg.Params[k] = q.Get(k)
		}
	}
	return cfg.FormatDSN(), nil
}
