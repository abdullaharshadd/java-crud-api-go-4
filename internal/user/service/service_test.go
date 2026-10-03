package user

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"migrated-app/internal/user/repository"
)

type fakeDao struct {
	repository.UserDao // embedded nil; unused methods panic

	saveFn       func(ctx context.Context, u *repository.User) (*repository.User, error)
	findAllFn    func(ctx context.Context) ([]*repository.User, error)
	findByIDFn   func(ctx context.Context, id int) (*repository.User, bool, error)
	deleteByIDFn func(ctx context.Context, id int) error
	findByNameFn func(ctx context.Context, name string) (*repository.User, bool, error)

	saveCalls   []*repository.User
	saveIDs     []int
	deleteCalls []int
	nameCalls   []string
}

func (f *fakeDao) Save(ctx context.Context, u *repository.User) (*repository.User, error) {
	f.saveCalls = append(f.saveCalls, u)
	if u != nil {
		f.saveIDs = append(f.saveIDs, u.ID)
	}
	return f.saveFn(ctx, u)
}
func (f *fakeDao) FindAll(ctx context.Context) ([]*repository.User, error) { return f.findAllFn(ctx) }
func (f *fakeDao) FindByID(ctx context.Context, id int) (*repository.User, bool, error) {
	return f.findByIDFn(ctx, id)
}
func (f *fakeDao) DeleteByID(ctx context.Context, id int) error {
	f.deleteCalls = append(f.deleteCalls, id)
	return f.deleteByIDFn(ctx, id)
}
func (f *fakeDao) FindByName(ctx context.Context, name string) (*repository.User, bool, error) {
	f.nameCalls = append(f.nameCalls, name)
	return f.findByNameFn(ctx, name)
}

func sp(s string) *string { return &s }

func hemraj() *repository.User {
	return &repository.User{ID: 3, Name: sp("hemraj"), Email: sp("hemrajmalhi1234@gmail.com"),
		About: sp("Sr"), Password: sp("root"), Role: sp("java developer")}
}

var errDB = errors.New("db down")

func TestNewService(t *testing.T) {
	if NewService(&fakeDao{}) == nil {
		t.Fatal("NewService returned nil")
	}
}

func TestGetUserNameByName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		dao      func(context.Context, string) (*repository.User, bool, error)
		wantUser bool
		wantOK   bool
		wantErr  []error
	}{
		{"found", "hemraj", func(context.Context, string) (*repository.User, bool, error) { return hemraj(), true, nil }, true, true, nil},
		{"not found", "nobody", func(context.Context, string) (*repository.User, bool, error) { return nil, false, nil }, false, false, nil},
		{"not unique", "dup", func(context.Context, string) (*repository.User, bool, error) {
			return nil, false, repository.ErrNotUnique
		}, false, false, []error{ErrNotUnique, repository.ErrNotUnique}},
		{"db error", "x", func(context.Context, string) (*repository.User, bool, error) { return nil, false, errDB }, false, false, []error{errDB}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeDao{findByNameFn: tt.dao}
			u, ok, err := NewService(f).GetUserNameByName(context.Background(), tt.input)
			if len(tt.wantErr) == 0 && err != nil {
				t.Fatalf("unexpected err %v", err)
			}
			for _, want := range tt.wantErr {
				if !errors.Is(err, want) {
					t.Errorf("err %v does not match %v", err, want)
				}
			}
			if len(tt.wantErr) > 0 && errors.Is(err, ErrNotUnique) != errors.Is(tt.wantErr[0], ErrNotUnique) {
				t.Errorf("unexpected ErrNotUnique match: %v", err)
			}
			if ok != tt.wantOK {
				t.Errorf("ok=%v want %v", ok, tt.wantOK)
			}
			if tt.wantUser {
				if u == nil || u.Name == nil || *u.Name != tt.input {
					t.Errorf("user name mismatch: %+v", u)
				}
			} else if u != nil {
				t.Errorf("expected nil user, got %+v", u)
			}
			if len(f.nameCalls) != 1 || f.nameCalls[0] != tt.input {
				t.Errorf("dao called with %v", f.nameCalls)
			}
			if len(f.saveCalls) != 0 || len(f.deleteCalls) != 0 {
				t.Error("read-only operation modified data")
			}
		})
	}
}

func TestSaveUser(t *testing.T) {
	tests := []struct {
		name    string
		in      *repository.User
		saveFn  func(context.Context, *repository.User) (*repository.User, error)
		wantID  int
		wantErr []error
		calls   int
	}{
		{"new user gets id", &repository.User{Name: sp("a")}, func(_ context.Context, u *repository.User) (*repository.User, error) {
			u.ID = 10
			return u, nil
		}, 10, nil, 1},
		{"existing user updated", hemraj(), func(_ context.Context, u *repository.User) (*repository.User, error) {
			c := *u
			return &c, nil
		}, 3, nil, 1},
		{"dao error", &repository.User{}, func(context.Context, *repository.User) (*repository.User, error) { return nil, errDB }, 0, []error{errDB}, 1},
		{"nil user", nil, nil, 0, []error{ErrValidation}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var returned *repository.User
			f := &fakeDao{}
			if tt.saveFn != nil {
				f.saveFn = func(ctx context.Context, u *repository.User) (*repository.User, error) {
					r, err := tt.saveFn(ctx, u)
					returned = r
					return r, err
				}
			}
			got, err := NewService(f).SaveUser(context.Background(), tt.in)
			if len(f.saveCalls) != tt.calls {
				t.Fatalf("save calls=%d want %d", len(f.saveCalls), tt.calls)
			}
			if len(tt.wantErr) > 0 {
				if got != nil {
					t.Errorf("expected nil result")
				}
				for _, w := range tt.wantErr {
					if !errors.Is(err, w) {
						t.Errorf("err %v not %v", err, w)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err %v", err)
			}
			if got != returned {
				t.Error("result is not repository's saved entity")
			}
			if got.ID != tt.wantID {
				t.Errorf("id=%d want %d", got.ID, tt.wantID)
			}
		})
	}
}

func TestFetchUserList(t *testing.T) {
	users := []*repository.User{hemraj(), {ID: 4, Name: sp("b")}}
	tests := []struct {
		name    string
		ret     []*repository.User
		err     error
		want    []*repository.User
		wantErr bool
	}{
		{"users exist", users, nil, users, false},
		{"nil from dao becomes empty", nil, nil, []*repository.User{}, false},
		{"empty", []*repository.User{}, nil, []*repository.User{}, false},
		{"error", nil, errDB, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeDao{findAllFn: func(context.Context) ([]*repository.User, error) { return tt.ret, tt.err }}
			got, err := NewService(f).FetchUserList(context.Background())
			if tt.wantErr {
				if !errors.Is(err, errDB) || got != nil {
					t.Fatalf("got %v, %v", got, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got == nil {
				t.Fatal("nil slice")
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v want %v", got, tt.want)
			}
		})
	}
}

func TestFetchUserByID(t *testing.T) {
	tests := []struct {
		name     string
		u        *repository.User
		ok       bool
		err      error
		notFound bool
	}{
		{"exists", hemraj(), true, nil, false},
		{"missing", nil, false, nil, true},
		{"db error", nil, false, errDB, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotID int
			f := &fakeDao{findByIDFn: func(_ context.Context, id int) (*repository.User, bool, error) {
				gotID = id
				return tt.u, tt.ok, tt.err
			}}
			got, err := NewService(f).FetchUserByID(context.Background(), 3)
			if gotID != 3 {
				t.Errorf("dao id=%d", gotID)
			}
			switch {
			case tt.notFound:
				if got != nil || !errors.Is(err, ErrUserNotFound) {
					t.Fatalf("got %v, %v", got, err)
				}
				var nf *NotFoundError
				if !errors.As(err, &nf) {
					t.Errorf("not *NotFoundError: %T", err)
				}
				if err.Error() != UserNotFoundMessage {
					t.Errorf("msg %q", err.Error())
				}
			case tt.err != nil:
				if got != nil || !errors.Is(err, tt.err) || errors.Is(err, ErrUserNotFound) {
					t.Fatalf("got %v, %v", got, err)
				}
			default:
				if err != nil || got != tt.u {
					t.Fatalf("got %v, %v", got, err)
				}
			}
		})
	}
}

func TestDeleteUser(t *testing.T) {
	tests := []struct {
		name    string
		daoErr  error
		wantErr []error
	}{
		{"exists", nil, nil},
		{"nothing deleted", repository.ErrNothingDeleted, []error{ErrNothingDeleted, repository.ErrNothingDeleted}},
		{"db error", errDB, []error{errDB}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeDao{deleteByIDFn: func(context.Context, int) error { return tt.daoErr }}
			err := NewService(f).DeleteUser(context.Background(), 7)
			if len(f.deleteCalls) != 1 || f.deleteCalls[0] != 7 {
				t.Errorf("delete calls %v", f.deleteCalls)
			}
			if len(tt.wantErr) == 0 && err != nil {
				t.Fatal(err)
			}
			for _, w := range tt.wantErr {
				if !errors.Is(err, w) {
					t.Errorf("err %v not %v", err, w)
				}
			}
			if tt.daoErr == errDB && errors.Is(err, ErrNothingDeleted) {
				t.Error("generic error should not map to ErrNothingDeleted")
			}
		})
	}
}

func TestDeleteThenFetchNotFound(t *testing.T) {
	store := map[int]*repository.User{3: hemraj()}
	f := &fakeDao{
		deleteByIDFn: func(_ context.Context, id int) error {
			if _, ok := store[id]; !ok {
				return repository.ErrNothingDeleted
			}
			delete(store, id)
			return nil
		},
		findByIDFn: func(_ context.Context, id int) (*repository.User, bool, error) {
			u, ok := store[id]
			return u, ok, nil
		},
	}
	s := NewService(f)
	if err := s.DeleteUser(context.Background(), 3); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FetchUserByID(context.Background(), 3); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("want not found, got %v", err)
	}
}

func TestUpdateUser(t *testing.T) {
	tests := []struct {
		name    string
		id      int
		in      *repository.User
		daoErr  error
		wantErr []error
	}{
		{"overrides id", 5, &repository.User{ID: 99, Name: sp("new"), Email: sp("e")}, nil, nil},
		{"sets id on zero", 3, &repository.User{Name: sp("x")}, nil, nil},
		{"dao error", 5, &repository.User{}, errDB, []error{errDB}},
		{"nil user", 5, nil, nil, []error{ErrValidation}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeDao{saveFn: func(_ context.Context, u *repository.User) (*repository.User, error) {
				if tt.daoErr != nil {
					return nil, tt.daoErr
				}
				return u, nil
			}}
			var before repository.User
			if tt.in != nil {
				before = *tt.in
			}
			err := NewService(f).UpdateUser(context.Background(), tt.id, tt.in)
			for _, w := range tt.wantErr {
				if !errors.Is(err, w) {
					t.Errorf("err %v not %v", err, w)
				}
			}
			if tt.in == nil {
				if len(f.saveCalls) != 0 {
					t.Error("save called for nil user")
				}
				return
			}
			if len(tt.wantErr) == 0 && err != nil {
				t.Fatal(err)
			}
			if tt.in.ID != tt.id {
				t.Errorf("id not mutated: %d", tt.in.ID)
			}
			if len(f.saveCalls) != 1 || f.saveCalls[0] != tt.in || f.saveIDs[0] != tt.id {
				t.Fatalf("save not called with the supplied user/id")
			}
			before.ID = tt.id
			if !reflect.DeepEqual(*f.saveCalls[0], before) {
				t.Errorf("fields changed: %+v", *f.saveCalls[0])
			}
		})
	}
}