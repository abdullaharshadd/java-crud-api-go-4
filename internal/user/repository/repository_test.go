package user

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// ---------------------------------------------------------------------------
// In-memory fake database/sql driver (mocks MySQL through driver interfaces).
// ---------------------------------------------------------------------------

type frow struct {
	id int64
	f  [5]driver.Value // name, email, password, role, about
}

type store struct {
	mu       sync.Mutex
	rows     []frow
	next     int64
	fail     map[string]error
	queries  []string
	execs    int
	snap     []frow
	snapNext int64
}

var (
	storesMu sync.Mutex
	stores   = map[string]*store{}
	dsnSeq   int64
)

type fakeDriver struct{}

func (fakeDriver) Open(name string) (driver.Conn, error) {
	storesMu.Lock()
	defer storesMu.Unlock()
	s, ok := stores[name]
	if !ok {
		return nil, fmt.Errorf("unknown dsn %q", name)
	}
	return &fakeConn{s: s}, nil
}

func init() { sql.Register("fakeuserdb", fakeDriver{}) }

type fakeConn struct{ s *store }

func (c *fakeConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("prepare not supported") }
func (c *fakeConn) Close() error                        { return nil }
func (c *fakeConn) Begin() (driver.Tx, error) {
	c.s.mu.Lock()
	defer c.s.mu.Unlock()
	c.s.snap = append([]frow(nil), c.s.rows...)
	c.s.snapNext = c.s.next
	return &fakeTx{s: c.s}, nil
}

func (c *fakeConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	return c.s.query(q, args)
}

func (c *fakeConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	return c.s.exec(q, args)
}

type fakeTx struct{ s *store }

func (t *fakeTx) Commit() error { return nil }
func (t *fakeTx) Rollback() error {
	t.s.mu.Lock()
	defer t.s.mu.Unlock()
	t.s.rows = t.s.snap
	t.s.next = t.s.snapNext
	return nil
}

type fakeResult struct{ lastID, affected int64 }

func (r fakeResult) LastInsertId() (int64, error) { return r.lastID, nil }
func (r fakeResult) RowsAffected() (int64, error) { return r.affected, nil }

type fakeRows struct {
	cols []string
	data [][]driver.Value
	i    int
}

func (r *fakeRows) Columns() []string { return r.cols }
func (r *fakeRows) Close() error      { return nil }
func (r *fakeRows) Next(dest []driver.Value) error {
	if r.i >= len(r.data) {
		return io.EOF
	}
	copy(dest, r.data[r.i])
	r.i++
	return nil
}

func (s *store) checkFail(q string) error {
	for sub, err := range s.fail {
		if strings.Contains(q, sub) {
			return err
		}
	}
	return nil
}

func argInt(nv driver.NamedValue) int64 {
	switch v := nv.Value.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	}
	return -1
}

func (s *store) query(q string, args []driver.NamedValue) (driver.Rows, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queries = append(s.queries, q)
	if err := s.checkFail(q); err != nil {
		return nil, err
	}
	switch {
	case strings.HasPrefix(q, "SELECT COUNT(*) FROM `user` WHERE user_id = ?"):
		var n int64
		id := argInt(args[0])
		for _, r := range s.rows {
			if r.id == id {
				n++
			}
		}
		return &fakeRows{cols: []string{"c"}, data: [][]driver.Value{{n}}}, nil
	case q == "SELECT COUNT(*) FROM `user`":
		return &fakeRows{cols: []string{"c"}, data: [][]driver.Value{{int64(len(s.rows))}}}, nil
	case strings.HasPrefix(q, "SELECT user_id FROM `user` WHERE user_id = ? FOR UPDATE"):
		out := &fakeRows{cols: []string{"user_id"}}
		id := argInt(args[0])
		for _, r := range s.rows {
			if r.id == id {
				out.data = append(out.data, []driver.Value{r.id})
			}
		}
		return out, nil
	case strings.HasPrefix(q, selectUsers):
		var sel []frow
		switch {
		case strings.Contains(q, "WHERE user_id IN"):
			want := map[int64]bool{}
			for _, a := range args {
				want[argInt(a)] = true
			}
			for _, r := range s.rows {
				if want[r.id] {
					sel = append(sel, r)
				}
			}
		case strings.Contains(q, "WHERE user_id = ?"):
			for _, r := range s.rows {
				if r.id == argInt(args[0]) {
					sel = append(sel, r)
				}
			}
		case strings.Contains(q, "WHERE user_name = ?"):
			for _, r := range s.rows {
				if r.f[0] != nil && r.f[0] == args[0].Value {
					sel = append(sel, r)
				}
			}
			if strings.HasSuffix(q, "LIMIT 2") && len(sel) > 2 {
				sel = sel[:2]
			}
		default:
			sel = append(sel, s.rows...)
		}
		if strings.HasSuffix(q, "LIMIT ? OFFSET ?") {
			lim := argInt(args[len(args)-2])
			off := argInt(args[len(args)-1])
			if off >= int64(len(sel)) {
				sel = nil
			} else {
				end := off + lim
				if end > int64(len(sel)) {
					end = int64(len(sel))
				}
				sel = sel[off:end]
			}
		}
		out := &fakeRows{cols: []string{"a", "b", "c", "d", "e", "f"}}
		for _, r := range sel {
			out.data = append(out.data, []driver.Value{r.id, r.f[0], r.f[1], r.f[2], r.f[3], r.f[4]})
		}
		return out, nil
	}
	return nil, fmt.Errorf("unexpected query %q", q)
}

func (s *store) exec(q string, args []driver.NamedValue) (driver.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queries = append(s.queries, q)
	s.execs++
	if err := s.checkFail(q); err != nil {
		return nil, err
	}
	switch {
	case strings.HasPrefix(q, "INSERT INTO `user`"):
		var r frow
		for i := 0; i < 5; i++ {
			r.f[i] = args[i].Value
		}
		if r.f[1] != nil {
			for _, e := range s.rows {
				if e.f[1] == r.f[1] {
					return nil, errors.New("duplicate entry for key uk_user_email")
				}
			}
		}
		s.next++
		r.id = s.next
		s.rows = append(s.rows, r)
		return fakeResult{lastID: r.id, affected: 1}, nil
	case strings.HasPrefix(q, "UPDATE `user`"):
		id := argInt(args[5])
		var n int64
		for i := range s.rows {
			if s.rows[i].id == id {
				for j := 0; j < 5; j++ {
					s.rows[i].f[j] = args[j].Value
				}
				n++
			}
		}
		return fakeResult{affected: n}, nil
	case strings.HasPrefix(q, "DELETE FROM `user` WHERE user_id IN"):
		want := map[int64]bool{}
		for _, a := range args {
			want[argInt(a)] = true
		}
		return s.deleteWhere(func(r frow) bool { return want[r.id] }), nil
	case strings.HasPrefix(q, "DELETE FROM `user` WHERE user_id = ?"):
		id := argInt(args[0])
		return s.deleteWhere(func(r frow) bool { return r.id == id }), nil
	case q == "DELETE FROM `user`":
		return s.deleteWhere(func(frow) bool { return true }), nil
	}
	return nil, fmt.Errorf("unexpected exec %q", q)
}

func (s *store) deleteWhere(match func(frow) bool) driver.Result {
	var keep []frow
	var n int64
	for _, r := range s.rows {
		if match(r) {
			n++
		} else {
			keep = append(keep, r)
		}
	}
	s.rows = keep
	return fakeResult{affected: n}
}

func (s *store) execCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.execs
}

func (s *store) lastQuery() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queries) == 0 {
		return ""
	}
	return s.queries[len(s.queries)-1]
}

func (s *store) rowCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.rows)
}

func newTestRepo(t *testing.T) (*MySQLRepository, *store) {
	t.Helper()
	dsn := fmt.Sprintf("db-%d", atomic.AddInt64(&dsnSeq, 1))
	s := &store{fail: map[string]error{}}
	storesMu.Lock()
	stores[dsn] = s
	storesMu.Unlock()
	db, err := sql.Open("fakeuserdb", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewMySQLRepository(db), s
}

func strp(s string) *string { return &s }

func mkUser(name, email string) *User {
	return &User{Name: strp(name), Email: strp(email), Password: strp("root"), Role: strp("java developer"), About: strp("Sr")}
}

func mustSave(t *testing.T, r *MySQLRepository, u *User) *User {
	t.Helper()
	saved, err := r.Save(context.Background(), u)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	return saved
}

func deref(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestFindByName(t *testing.T) {
	dbErr := errors.New("boom")
	tests := []struct {
		name      string
		seed      []*User
		fail      map[string]error
		lookup    string
		wantFound bool
		wantErr   error
		anyErr    bool
	}{
		{
			name:      "existing user hemraj",
			seed:      []*User{mkUser("other", "o@x.com"), mkUser("hemraj", "hemrajmalhi1234@gmail.com")},
			lookup:    "hemraj",
			wantFound: true,
		},
		{
			name:   "no user with name",
			seed:   []*User{mkUser("hemraj", "hemrajmalhi1234@gmail.com")},
			lookup: "nobody",
		},
		{
			name:   "exact match only",
			seed:   []*User{mkUser("hemraj", "a@x.com")},
			lookup: "Hemraj",
		},
		{
			name:    "duplicate names not unique",
			seed:    []*User{mkUser("dup", "a@x.com"), mkUser("dup", "b@x.com"), mkUser("dup", "c@x.com")},
			lookup:  "dup",
			wantErr: ErrNotUnique,
		},
		{
			name:    "query error",
			fail:    map[string]error{"WHERE user_name": dbErr},
			lookup:  "x",
			wantErr: dbErr,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, s := newTestRepo(t)
			for _, u := range tc.seed {
				mustSave(t, r, u)
			}
			for k, v := range tc.fail {
				s.fail[k] = v
			}
			before, rowsBefore := s.execCount(), s.rowCount()
			u, found, err := r.FindByName(context.Background(), tc.lookup)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
			} else if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if found != tc.wantFound {
				t.Fatalf("found = %v, want %v", found, tc.wantFound)
			}
			if !tc.wantFound && u != nil {
				t.Fatalf("expected nil user, got %v", u)
			}
			if tc.wantFound && (u == nil || deref(u.Name) != tc.lookup) {
				t.Fatalf("name = %v, want %q", u, tc.lookup)
			}
			if s.execCount() != before || s.rowCount() != rowsBefore {
				t.Fatalf("FindByName mutated data")
			}
		})
	}
}

func TestFindByNameReturnsAllFields(t *testing.T) {
	r, _ := newTestRepo(t)
	mustSave(t, r, mkUser("a", "a@x.com"))
	mustSave(t, r, mkUser("b", "b@x.com"))
	saved := mustSave(t, r, mkUser("hemraj", "hemrajmalhi1234@gmail.com"))
	if saved.ID != 3 {
		t.Fatalf("id = %d, want 3", saved.ID)
	}
	u, ok, err := r.FindByName(context.Background(), "hemraj")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if u.ID != 3 || deref(u.Email) != "hemrajmalhi1234@gmail.com" || deref(u.About) != "Sr" ||
		deref(u.Password) != "root" || deref(u.Role) != "java developer" {
		t.Fatalf("unexpected user %+v", u)
	}
}

func TestSave(t *testing.T) {
	ctx := context.Background()

	t.Run("nil entity", func(t *testing.T) {
		r, s := newTestRepo(t)
		if _, err := r.Save(ctx, nil); err == nil {
			t.Fatal("expected error")
		}
		if s.rowCount() != 0 {
			t.Fatal("row inserted")
		}
	})

	t.Run("new entity gets id and is findable", func(t *testing.T) {
		r, _ := newTestRepo(t)
		u := mkUser("n", "n@x.com")
		saved := mustSave(t, r, u)
		if saved.ID == 0 || u.ID != saved.ID {
			t.Fatalf("id not assigned: saved=%d orig=%d", saved.ID, u.ID)
		}
		got, ok, err := r.FindByID(ctx, saved.ID)
		if err != nil || !ok || deref(got.Name) != "n" {
			t.Fatalf("find: %v %v %v", got, ok, err)
		}
	})

	t.Run("nil fields stored as NULL", func(t *testing.T) {
		r, _ := newTestRepo(t)
		saved := mustSave(t, r, &User{Name: strp("only")})
		got, _, _ := r.FindByID(ctx, saved.ID)
		if got.Email != nil || got.About != nil || got.Role != nil || got.Password != nil {
			t.Fatalf("expected nil fields, got %+v", got)
		}
	})

	t.Run("update existing", func(t *testing.T) {
		r, s := newTestRepo(t)
		saved := mustSave(t, r, mkUser("old", "o@x.com"))
		upd := &User{ID: saved.ID, Name: strp("new"), Email: strp("o@x.com")}
		res := mustSave(t, r, upd)
		if res.ID != saved.ID || res == upd {
			t.Fatalf("unexpected merge result %+v", res)
		}
		got, _, _ := r.FindByID(ctx, saved.ID)
		if deref(got.Name) != "new" || got.About != nil {
			t.Fatalf("not updated: %+v", got)
		}
		if s.rowCount() != 1 {
			t.Fatalf("rows = %d", s.rowCount())
		}
	})

	t.Run("merge with unknown id inserts new row", func(t *testing.T) {
		r, s := newTestRepo(t)
		u := &User{ID: 99, Name: strp("m")}
		res := mustSave(t, r, u)
		if res.ID == 99 || res.ID == 0 {
			t.Fatalf("id = %d", res.ID)
		}
		if u.ID != 99 {
			t.Fatalf("original mutated: %d", u.ID)
		}
		if ok, _ := r.ExistsByID(ctx, res.ID); !ok {
			t.Fatal("not findable")
		}
		if s.rowCount() != 1 {
			t.Fatal("expected 1 row")
		}
	})

	errCases := []struct {
		name string
		fail string
		id   bool
	}{
		{"insert error", "INSERT INTO", false},
		{"update error", "UPDATE `user`", true},
		{"lock error", "FOR UPDATE", true},
	}
	for _, tc := range errCases {
		t.Run(tc.name, func(t *testing.T) {
			r, s := newTestRepo(t)
			u := mkUser("x", "x@x.com")
			if tc.id {
				u = mustSave(t, r, u)
			}
			s.fail[tc.fail] = errors.New("db down")
			if _, err := r.Save(ctx, u); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestSaveAll(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name     string
		users    []*User
		wantErr  bool
		wantRows int
	}{
		{"all saved", []*User{mkUser("a", "a@x.com"), mkUser("b", "b@x.com")}, false, 2},
		{"empty", nil, false, 0},
		{"nil element rolls back", []*User{mkUser("a", "a@x.com"), nil}, true, 0},
		{"duplicate email rolls back", []*User{mkUser("a", "d@x.com"), mkUser("b", "d@x.com")}, true, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, s := newTestRepo(t)
			out, err := r.SaveAll(ctx, tc.users)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v", err)
			}
			if !tc.wantErr {
				if len(out) != len(tc.users) {
					t.Fatalf("len = %d", len(out))
				}
				for _, u := range out {
					if u.ID == 0 {
						t.Fatal("id not assigned")
					}
				}
			}
			if s.rowCount() != tc.wantRows {
				t.Fatalf("rows = %d, want %d", s.rowCount(), tc.wantRows)
			}
		})
	}
}

func TestFindByID(t *testing.T) {
	ctx := context.Background()
	dbErr := errors.New("boom")
	tests := []struct {
		name      string
		id        func(saved int) int
		fail      bool
		wantFound bool
		wantErr   bool
	}{
		{"exists", func(s int) int { return s }, false, true, false},
		{"missing", func(s int) int { return s + 100 }, false, false, false},
		{"db error", func(s int) int { return s }, true, false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, s := newTestRepo(t)
			saved := mustSave(t, r, mkUser("a", "a@x.com"))
			if tc.fail {
				s.fail["WHERE user_id = ?"] = dbErr
			}
			u, ok, err := r.FindByID(ctx, tc.id(saved.ID))
			if (err != nil) != tc.wantErr || ok != tc.wantFound {
				t.Fatalf("ok=%v err=%v", ok, err)
			}
			if tc.wantErr && !errors.Is(err, dbErr) {
				t.Fatalf("err not wrapped: %v", err)
			}
			if tc.wantFound && (u == nil || u.ID != saved.ID) {
				t.Fatalf("u = %v", u)
			}
			if !tc.wantFound && u != nil {
				t.Fatal("expected nil")
			}
		})
	}
}

func TestExistsByIDAndCount(t *testing.T) {
	ctx := context.Background()
	r, s := newTestRepo(t)
	all, err := r.FindAll(ctx)
	if err != nil || all == nil || len(all) != 0 {
		t.Fatalf("empty FindAll = %v, %v", all, err)
	}
	a := mustSave(t, r, mkUser("a", "a@x.com"))
	mustSave(t, r, mkUser("b", "b@x.com"))

	tests := []struct {
		id   int
		want bool
	}{{a.ID, true}, {a.ID + 50, false}}
	for _, tc := range tests {
		got, err := r.ExistsByID(ctx, tc.id)
		if err != nil || got != tc.want {
			t.Fatalf("ExistsByID(%d) = %v, %v", tc.id, got, err)
		}
	}
	n, err := r.Count(ctx)
	if err != nil {
		t.Fatal(err)
	}
	all, err = r.FindAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != int64(len(all)) || n != 2 {
		t.Fatalf("count %d vs findAll %d", n, len(all))
	}

	s.fail["COUNT(*)"] = errors.New("x")
	if _, err := r.Count(ctx); err == nil {
		t.Fatal("expected count error")
	}
	if _, err := r.ExistsByID(ctx, 1); err == nil {
		t.Fatal("expected exists error")
	}
	s.fail["SELECT user_id, user_name"] = errors.New("x")
	if _, err := r.FindAll(ctx); err == nil {
		t.Fatal("expected findAll error")
	}
}

func TestFindAllSorted(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name      string
		sort      Sort
		wantOrder string
		wantErr   error
	}{
		{"no sort", nil, "", nil},
		{"single asc", Sort{{Property: "name"}}, " ORDER BY user_name ASC", nil},
		{"multi", Sort{{"email", Desc}, {"id", Asc}}, " ORDER BY user_email DESC, user_id ASC", nil},
		{"invalid property", Sort{{Property: "bogus"}}, "", ErrInvalidSortProperty},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, s := newTestRepo(t)
			mustSave(t, r, mkUser("a", "a@x.com"))
			users, err := r.FindAllSorted(ctx, tc.sort)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v", err)
				}
				return
			}
			if err != nil || len(users) != 1 {
				t.Fatalf("users=%v err=%v", users, err)
			}
			if q := s.lastQuery(); q != selectUsers+tc.wantOrder {
				t.Fatalf("query = %q", q)
			}
		})
	}
}

func TestFindAllPaged(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name      string
		req       PageRequest
		wantErr   error
		wantLen   int
		wantPages int
	}{
		{"first page", PageRequest{Page: 0, Size: 2}, nil, 2, 3},
		{"last partial page", PageRequest{Page: 2, Size: 2}, nil, 1, 3},
		{"beyond end", PageRequest{Page: 5, Size: 2}, nil, 0, 3},
		{"sorted", PageRequest{Page: 0, Size: 5, Sort: Sort{{"role", Desc}}}, nil, 5, 1},
		{"negative page", PageRequest{Page: -1, Size: 2}, ErrInvalidPageRequest, 0, 0},
		{"zero size", PageRequest{Page: 0, Size: 0}, ErrInvalidPageRequest, 0, 0},
		{"invalid sort", PageRequest{Page: 0, Size: 1, Sort: Sort{{Property: "x"}}}, ErrInvalidSortProperty, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := newTestRepo(t)
			for i := 0; i < 5; i++ {
				mustSave(t, r, mkUser(fmt.Sprint("u", i), fmt.Sprintf("u%d@x.com", i)))
			}
			p, err := r.FindAllPaged(ctx, tc.req)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(p.Content) != tc.wantLen || p.TotalPages != tc.wantPages || p.TotalElements != 5 ||
				p.Number != tc.req.Page || p.Size != tc.req.Size {
				t.Fatalf("page = %+v", p)
			}
		})
	}
}

func TestFindAllByID(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestRepo(t)
	a := mustSave(t, r, mkUser("a", "a@x.com"))
	b := mustSave(t, r, mkUser("b", "b@x.com"))
	tests := []struct {
		name string
		ids  []int
		want int
	}{
		{"empty", nil, 0},
		{"both", []int{a.ID, b.ID}, 2},
		{"missing skipped", []int{a.ID, 999}, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := r.FindAllByID(ctx, tc.ids)
			if err != nil || got == nil || len(got) != tc.want {
				t.Fatalf("got %v err %v", got, err)
			}
		})
	}
}

func TestDeleteByID(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		missing bool
		wantErr error
	}{
		{"existing", false, nil},
		{"missing", true, ErrNothingDeleted},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, s := newTestRepo(t)
			u := mustSave(t, r, mkUser("a", "a@x.com"))
			id := u.ID
			if tc.missing {
				id += 10
			}
			err := r.DeleteByID(ctx, id)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v", err)
				}
				if s.rowCount() != 1 {
					t.Fatal("row removed")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if ok, _ := r.ExistsByID(ctx, id); ok {
				t.Fatal("still exists")
			}
		})
	}
}

func TestDelete(t *testing.T) {
	ctx := context.Background()
	r, s := newTestRepo(t)
	u := mustSave(t, r, mkUser("a", "a@x.com"))
	if err := r.Delete(ctx, nil); err == nil {
		t.Fatal("expected nil entity error")
	}
	before := s.execCount()
	if err := r.Delete(ctx, &User{}); err != nil || s.execCount() != before {
		t.Fatalf("new entity: err=%v", err)
	}
	if err := r.Delete(ctx, &User{ID: 777}); err != nil {
		t.Fatalf("gone entity: %v", err)
	}
	if err := r.Delete(ctx, u); err != nil || s.rowCount() != 0 {
		t.Fatalf("delete: %v rows=%d", err, s.rowCount())
	}
	s.fail["DELETE"] = errors.New("x")
	if err := r.Delete(ctx, &User{ID: 1}); err == nil {
		t.Fatal("expected error")
	}
}

func TestDeleteAllByIDAndDeleteAll(t *testing.T) {
	ctx := context.Background()
	r, s := newTestRepo(t)
	a := mustSave(t, r, mkUser("a", "a@x.com"))
	b := mustSave(t, r, mkUser("b", "b@x.com"))
	mustSave(t, r, mkUser("c", "c@x.com"))

	before := s.execCount()
	if err := r.DeleteAllByID(ctx, nil); err != nil || s.execCount() != before {
		t.Fatalf("empty: %v", err)
	}
	if err := r.DeleteAllByID(ctx, []int{a.ID, b.ID}); err != nil || s.rowCount() != 1 {
		t.Fatalf("err=%v rows=%d", err, s.rowCount())
	}
	if err := r.DeleteAll(ctx); err != nil || s.rowCount() != 0 {
		t.Fatalf("err=%v rows=%d", err, s.rowCount())
	}
	s.fail["DELETE"] = errors.New("x")
	if err := r.DeleteAll(ctx); err == nil {
		t.Fatal("expected error")
	}
	if err := r.DeleteAllByID(ctx, []int{1}); err == nil {
		t.Fatal("expected error")
	}
}

func TestHelpers(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{{0, ""}, {1, "?"}, {3, "?, ?, ?"}}
	for _, tc := range tests {
		if got := placeholders(tc.n); got != tc.want {
			t.Errorf("placeholders(%d) = %q", tc.n, got)
		}
	}
	args := intsToArgs([]int{1, 2})
	if len(args) != 2 || args[0] != 1 || args[1] != 2 {
		t.Errorf("intsToArgs = %v", args)
	}
	if p := nullToPtr(sql.NullString{}); p != nil {
		t.Error("expected nil")
	}
	if ns := ptrToNull(strp("x")); !ns.Valid || ns.String != "x" {
		t.Error("ptrToNull")
	}
}