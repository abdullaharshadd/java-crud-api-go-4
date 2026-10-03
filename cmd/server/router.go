package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	mysql "github.com/go-sql-driver/mysql"
	"github.com/rs/zerolog/log"

	"migrated-app/internal/config"
	"migrated-app/internal/httpx"
	"migrated-app/internal/user/handler"
	"migrated-app/internal/user/repository"
)

// errUserNotFound mirrors user.ErrUserNotFound. internal/user cannot be
// imported because its files declare conflicting package clauses, so the
// sentinel (with the identical client-facing message) is defined here.
var errUserNotFound = errors.New("User are not available")

var errDBNotConfigured = errors.New("database is not configured")

func buildRouter() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	var dao repository.UserDao
	if db, err := openDB(); err != nil {
		log.Error().Err(err).Msg("database unavailable; user routes will return errors")
	} else {
		dao = repository.NewMySQLRepository(db)
	}

	svc := &userService{dao: dao}
	errs := httpx.NewErrorHandler(errUserNotFound)
	h, err := handler.New(svc, errs)
	if err != nil {
		log.Fatal().Err(err).Msg("build user handler")
	}

	// Register the user routes directly on the chi router.
	h.RegisterChi(r)
	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		httpx.WriteDefaultError(w, req, http.StatusNotFound)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteEmpty(w, http.StatusMethodNotAllowed)
	})

	return r
}

func openDB() (*sql.DB, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	dsn, err := mysqlDSN(cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := user.EnsureSchema(ctx, db); err != nil {
		log.Error().Err(err).Msg("ensure user schema")
	}
	return db, nil
}

// mysqlDSN converts DATABASE_URL (mysql://..., jdbc:mysql://... or a native
// driver DSN) into a go-sql-driver/mysql DSN. All values come from the env.
func mysqlDSN(raw string) (string, error) {
	raw = strings.TrimPrefix(strings.TrimSpace(raw), "jdbc:")
	if raw == "" {
		return "", errors.New("DATABASE_URL is not set")
	}
	if !strings.HasPrefix(raw, "mysql://") {
		if _, err := mysql.ParseDSN(raw); err != nil {
			return "", fmt.Errorf("invalid DATABASE_URL: %w", err)
		}
		return raw, nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid DATABASE_URL: %w", err)
	}
	c := mysql.NewConfig()
	c.Net = "tcp"
	c.Addr = u.Host
	if u.User != nil {
		c.User = u.User.Username()
		if pw, ok := u.User.Password(); ok {
			c.Passwd = pw
		}
	}
	c.DBName = strings.TrimPrefix(u.Path, "/")
	if q := u.Query(); len(q) > 0 {
		c.Params = make(map[string]string, len(q))
		for k := range q {
			c.Params[k] = q.Get(k)
		}
	}
	return c.FormatDSN(), nil
}

// userService implements handler.Service on top of repository.UserDao.
type userService struct {
	dao repository.UserDao
}

func (s *userService) ready() error {
	if s.dao == nil {
		return errDBNotConfigured
	}
	return nil
}

func (s *userService) SaveUser(ctx context.Context, u *repository.User) (*repository.User, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	return s.dao.Save(ctx, u)
}

func (s *userService) FetchUserList(ctx context.Context) ([]*repository.User, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	users, err := s.dao.FindAll(ctx)
	if err != nil {
		return nil, err
	}
	if users == nil {
		users = make([]*repository.User, 0)
	}
	return users, nil
}

func (s *userService) FetchUserByID(ctx context.Context, id int) (*repository.User, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	u, ok, err := s.dao.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errUserNotFound
	}
	return u, nil
}

func (s *userService) DeleteUser(ctx context.Context, id int) error {
	if err := s.ready(); err != nil {
		return err
	}
	return s.dao.DeleteByID(ctx, id)
}

func (s *userService) UpdateUser(ctx context.Context, id int, u *repository.User) error {
	if err := s.ready(); err != nil {
		return err
	}
	if u == nil {
		return errors.New("user must not be nil")
	}
	u.ID = id
	_, err := s.dao.Save(ctx, u)
	return err
}

func (s *userService) GetUserNameByName(ctx context.Context, name string) (*repository.User, bool, error) {
	if err := s.ready(); err != nil {
		return nil, false, err
	}
	return s.dao.FindByName(ctx, name)
}