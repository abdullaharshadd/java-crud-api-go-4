package user

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"migrated-app/internal/user/repository"
)

// fakeDao is an in-memory repository.UserDao used to test the service layer.
type fakeDao struct {
	users  map[int]*repository.User
	nextID int

	saveErr     error
	findAllErr  error
	findByIDErr error
	deleteErr   error
	findNameErr error
	nilFindAll  bool

	saveCalls   int
	deleteCalls int
	lastSaved   *repository.User
}

func newFakeDao() *fakeDao {
	return &fakeDao{users: map[int]*repository.User{}, nextID: 1}
}

func copyUser(u *repository.User) *repository.User {
	c := *u
	return &c
}

func (f *fakeDao) Save(ctx context.Context, u *repository.User) (*repository.User, error) {
	f.saveCalls++
	f.lastSaved = u
	if f.saveErr != nil {
		return nil, f.saveErr
	}
	if u.ID == 0 {
		u.ID = f.nextID
		f.nextID++
		f.users[u.ID] = copyUser(u)
		return u, nil
	}
	if _, ok := f.users[u.ID]; ok {
		f.users[u.ID] = copyUser(u)
		return copyUser(u), nil
	}
	m := copyUser(u)
	m.ID = f.nextID
	f.nextID++
	f.users[m.ID] = copyUser(m)
	return m, nil
}

func (f *fakeDao) SaveAll(ctx context.Context, users []*repository.User) ([]*repository.User, error) {
	out := []*repository.User{}
	for _, u := range users {
		s, err := f.Save(ctx, u)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func (f *fakeDao) FindByID(ctx context.Context, id int) (*repository.User, bool, error) {
	if f.findByIDErr != nil {
		return nil, false, f.findByIDErr
	}
	u, ok := f.users[id]
	if !ok {
		return nil, false, nil
	}
	return copyUser(u), true, nil
}

func (f *fakeDao) ExistsByID(ctx context.Context, id int) (bool, error) {
	_, ok := f.users[id]
	return ok, nil
}

func (f *fakeDao) FindAll(ctx context.Context) ([]*repository.User, error) {
	if f.findAllErr != nil {
		return nil, f.findAllErr
	}
	if f.nilFindAll {
		return nil, nil
	}
	out := make([]*repository.User, 0, len(f.users))
	for i := 1; i < f.nextID; i++ {
		if u, ok := f.users[i]; ok {
			out = append(out, copyUser(u))
		}
	}
	return out, nil
}

func (f *fakeDao) FindAllSorted(ctx context.Context, s repository.Sort) ([]*repository.User, error) {
	return f.FindAll(ctx)
}

func (f *fakeDao) FindAllPaged(ctx context.Context, p repository.PageRequest) (repository.Page, error) {
	return repository.Page{}, nil
}

func (f *fakeDao) FindAllByID(ctx context.Context, ids []int) ([]*repository.User, error) {
	return nil, nil
}

func (f *fakeDao) Count(ctx context.Context) (int64, error) { return int64(len(f.users)), nil }

func (f *fakeDao) DeleteByID(ctx context.Context, id int) error {
	f.deleteCalls++
	if f.deleteErr != nil {
		return f.deleteErr
	}
	if _, ok := f.users[id]; !ok {
		return errors.New("user repository: wrap: " + repository.ErrNothingDeleted.Error())
	}
	delete(f.users, id)
	return nil
}

func (f *fakeDao) Delete(ctx context.Context, u *repository.User) error      { return nil }
func (f *fakeDao) DeleteAllByID(ctx context.Context, ids []int) error         { return nil }
func (f *fakeDao) DeleteAll(ctx context.Context) error                        { f.users = map[int]*repository.User{}; return nil }

func (f *fakeDao) FindByName(ctx context.Context, name string) (*repository.User, bool, error) {
	if f.findNameErr != nil {
		return nil, false, f.findNameErr
	}
	var found []*repository.User
	for _, u := range f.users {
		if u.Name != nil && *u.Name == name {
			found = append(found, u)
		}
	}
	switch len(found) {
	case 0:
		return nil, false, nil
	case 1:
		return copyUser(found[0]), true, nil
	default:
		return nil, false, repository.ErrNotUnique
	}
}

var _ repository.UserDao = (*fakeDao)(nil)

func sp(s string) *string { return &s }

// seed stores users directly in the fake, bypassing the service.
func (f *fakeDao) seed(names ...string) {
	for _, n := range names {
		u := &repository.User{ID: f.nextID, Name: sp(n), Email: sp(n + "@x.io")}
		f.users[u.ID] = u
		f.nextID++
	}
}

func TestSaveUser(t *testing.T) {
	dbErr := errors.New("duplicate entry for uk_user_email")
	tests := []struct {
		name      string
		setup     func(*fakeDao)
		input     *repository.User
		wantErrIs error
		wantID    int
		wantName  string
		wantCount int
	}{
		{
			name:      "new user gets generated id",
			setup:     func(f *fakeDao) {},
			input:     &repository.User{Name: sp("alice"), Email: sp("a@x.io")},
			wantID:    1,
			wantName:  "alice",
			wantCount: 1,
		},
		{
			name:      "existing id is overwritten",
			setup:     func(f *fakeDao) { f.seed("old") },
			input:     &repository.User{ID: 1, Name: sp("new"), Email: sp("n@x.io")},
			wantID:    1,
			wantName:  "new",
			wantCount: 1,
		},
		{
			name:      "persistence error propagated",
			setup:     func(f *fakeDao) { f.saveErr = dbErr },
			input:     &repository.User{Name: sp("dup")},
			wantErrIs: dbErr,
			wantCount: 0,
		},
		{
			name:      "nil user is validation error",
			setup:     func(f *fakeDao) {},
			input:     nil,
			wantErrIs: ErrValidation,
			wantCount: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dao := newFakeDao()
			tt.setup(dao)
			svc := NewService(dao)
			got, err := svc.SaveUser(context.Background(), tt.input)
			if tt.wantErrIs != nil {
				if !errors.Is(err, tt.wantErrIs) {
					t.Fatalf("err = %v, want %v", err, tt.wantErrIs)
				}
				if got != nil {
					t.Fatalf("expected nil user, got %+v", got)
				}
				if tt.input == nil && dao.saveCalls != 0 {
					t.Fatalf("dao.Save called for nil user")
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected err: %v", err)
				}
				if got.ID != tt.wantID || got.Name == nil || *got.Name != tt.wantName {
					t.Fatalf("got %+v", got)
				}
				fetched, err := svc.FetchUserByID(context.Background(), got.ID)
				if err != nil || !reflect.DeepEqual(fetched, got) {
					t.Fatalf("fetch after save = %+v, %v; want %+v", fetched, err, got)
				}
			}
			if len(dao.users) != tt.wantCount {
				t.Fatalf("stored = %d, want %d", len(dao.users), tt.wantCount)
			}
		})
	}
}

func TestFetchUserList(t *testing.T) {
	dbErr := errors.New("db down")
	tests := []struct {
		name    string
		setup   func(*fakeDao)
		wantLen int
		wantErr error
	}{
		{"users exist", func(f *fakeDao) { f.seed("a", "b", "c") }, 3, nil},
		{"empty store", func(f *fakeDao) {}, 0, nil},
		{"dao returns nil slice", func(f *fakeDao) { f.nilFindAll = true }, 0, nil},
		{"dao error", func(f *fakeDao) { f.findAllErr = dbErr }, 0, dbErr},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dao := newFakeDao()
			tt.setup(dao)
			before := len(dao.users)
			got, err := NewService(dao).FetchUserList(context.Background())
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got == nil {
				t.Fatal("list must never be nil")
			}
			if len(got) != tt.wantLen {
				t.Fatalf("len = %d, want %d", len(got), tt.wantLen)
			}
			if len(dao.users) != before || dao.saveCalls != 0 || dao.deleteCalls != 0 {
				t.Fatal("read operation mutated store")
			}
		})
	}
}

func TestFetchUserByID(t *testing.T) {
	dbErr := errors.New("db down")
	tests := []struct {
		name         string
		setup        func(*fakeDao)
		id           int
		wantNotFound bool
		wantErr      error
	}{
		{"existing", func(f *fakeDao) { f.seed("a", "b") }, 2, false, nil},
		{"missing", func(f *fakeDao) { f.seed("a") }, 99, true, nil},
		{"dao error", func(f *fakeDao) { f.findByIDErr = dbErr }, 1, false, dbErr},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dao := newFakeDao()
			tt.setup(dao)
			got, err := NewService(dao).FetchUserByID(context.Background(), tt.id)
			switch {
			case tt.wantNotFound:
				if !errors.Is(err, ErrUserNotFound) {
					t.Fatalf("err = %v, want ErrUserNotFound", err)
				}
				var nf *NotFoundError
				if !errors.As(err, &nf) {
					t.Fatalf("err is not *NotFoundError: %T", err)
				}
				if err.Error() != UserNotFoundMessage {
					t.Fatalf("message = %q", err.Error())
				}
				if got != nil {
					t.Fatal("expected nil user")
				}
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v", err)
				}
				if errors.Is(err, ErrUserNotFound) {
					t.Fatal("db error must not be not-found")
				}
			default:
				if err != nil || got == nil || got.ID != tt.id {
					t.Fatalf("got %+v, %v", got, err)
				}
			}
		})
	}
}

func TestDeleteUser(t *testing.T) {
	dbErr := errors.New("db down")
	tests := []struct {
		name          string
		setup         func(*fakeDao)
		id            int
		wantErr       error
		wantNothing   bool
		wantRemaining int
	}{
		{"existing", func(f *fakeDao) { f.seed("a", "b") }, 1, nil, false, 1},
		{"missing", func(f *fakeDao) { f.seed("a") }, 42, nil, true, 1},
		{"dao error", func(f *fakeDao) { f.seed("a"); f.deleteErr = dbErr }, 1, dbErr, false, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dao := newFakeDao()
			tt.setup(dao)
			svc := NewService(dao)
			err := svc.DeleteUser(context.Background(), tt.id)
			switch {
			case tt.wantNothing:
				if err == nil {
					t.Fatal("expected error")
				}
				if errors.Is(err, ErrUserNotFound) {
					t.Fatal("delete must not report user-not-found")
				}
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) || errors.Is(err, ErrNothingDeleted) {
					t.Fatalf("err = %v", err)
				}
			default:
				if err != nil {
					t.Fatalf("unexpected err: %v", err)
				}
				if _, err := svc.FetchUserByID(context.Background(), tt.id); !errors.Is(err, ErrUserNotFound) {
					t.Fatalf("after delete err = %v", err)
				}
			}
			if len(dao.users) != tt.wantRemaining {
				t.Fatalf("remaining = %d, want %d", len(dao.users), tt.wantRemaining)
			}
		})
	}
}

// wrappingDeleteDao returns a properly wrapped repository.ErrNothingDeleted.
type wrappingDeleteDao struct{ *fakeDao }

func (w wrappingDeleteDao) DeleteByID(ctx context.Context, id int) error {
	return errors.Join(errors.New("user repository: delete"), repository.ErrNothingDeleted)
}

func TestDeleteUserMapsNothingDeleted(t *testing.T) {
	err := NewService(wrappingDeleteDao{newFakeDao()}).DeleteUser(context.Background(), 7)
	if !errors.Is(err, ErrNothingDeleted) {
		t.Fatalf("err = %v, want ErrNothingDeleted", err)
	}
	if !errors.Is(err, repository.ErrNothingDeleted) {
		t.Fatalf("err = %v, want repository.ErrNothingDeleted preserved", err)
	}
}

func TestUpdateUser(t *testing.T) {
	dbErr := errors.New("db down")
	tests := []struct {
		name      string
		setup     func(*fakeDao)
		id        int
		input     *repository.User
		wantErr   error
		check     func(t *testing.T, f *fakeDao)
	}{
		{
			name:  "existing id updated",
			setup: func(f *fakeDao) { f.seed("a", "b") },
			id:    2,
			input: &repository.User{ID: 999, Name: sp("bee")},
			check: func(t *testing.T, f *fakeDao) {
				if f.lastSaved.ID != 2 {
					t.Fatalf("saved id = %d, want 2", f.lastSaved.ID)
				}
				if *f.users[2].Name != "bee" || *f.users[1].Name != "a" || len(f.users) != 2 {
					t.Fatalf("unexpected store: %+v", f.users)
				}
			},
		},
		{
			name:  "missing id inserts new row",
			setup: func(f *fakeDao) { f.seed("a") },
			id:    50,
			input: &repository.User{Name: sp("z")},
			check: func(t *testing.T, f *fakeDao) {
				if len(f.users) != 2 || *f.users[1].Name != "a" {
					t.Fatalf("unexpected store: %+v", f.users)
				}
			},
		},
		{
			name:    "nil user",
			setup:   func(f *fakeDao) {},
			id:      1,
			input:   nil,
			wantErr: ErrValidation,
			check: func(t *testing.T, f *fakeDao) {
				if f.saveCalls != 0 {
					t.Fatal("save should not be called")
				}
			},
		},
		{
			name:    "dao error",
			setup:   func(f *fakeDao) { f.saveErr = dbErr },
			id:      1,
			input:   &repository.User{Name: sp("x")},
			wantErr: dbErr,
			check:   func(t *testing.T, f *fakeDao) {},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dao := newFakeDao()
			tt.setup(dao)
			err := NewService(dao).UpdateUser(context.Background(), tt.id, tt.input)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if err != nil && errors.Is(err, ErrUserNotFound) {
				t.Fatal("update must not report user-not-found")
			}
			tt.check(t, dao)
		})
	}
}

func TestGetUserNameByName(t *testing.T) {
	dbErr := errors.New("db down")
	tests := []struct {
		name      string
		setup     func(*fakeDao)
		query     string
		wantNil   bool
		wantErrIs []error
	}{
		{"match", func(f *fakeDao) { f.seed("alice", "bob") }, "bob", false, nil},
		{"no match", func(f *fakeDao) { f.seed("alice") }, "carol", true, nil},
		{"multiple", func(f *fakeDao) { f.seed("dup", "dup") }, "dup", true, []error{ErrNotUnique, repository.ErrNotUnique}},
		{"dao error", func(f *fakeDao) { f.findNameErr = dbErr }, "x", true, []error{dbErr}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dao := newFakeDao()
			tt.setup(dao)
			before := len(dao.users)
			got, err := NewService(dao).GetUserNameByName(context.Background(), tt.query)
			if len(tt.wantErrIs) > 0 {
				for _, e := range tt.wantErrIs {
					if !errors.Is(err, e) {
						t.Fatalf("err = %v, want %v", err, e)
					}
				}
			} else if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if tt.wantNil {
				if got != nil {
					t.Fatalf("want nil, got %+v", got)
				}
			} else if got == nil || got.Name == nil || *got.Name != tt.query {
				t.Fatalf("got %+v", got)
			}
			if len(dao.users) != before || dao.saveCalls != 0 || dao.deleteCalls != 0 {
				t.Fatal("read operation mutated store")
			}
		})
	}
}