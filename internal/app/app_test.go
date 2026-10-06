package app

import (
	"strings"
	"sync"
	"testing"
	"time"

	"mantis-report-watcher/internal/mantis"
)

type fakePlat struct {
	mu    sync.Mutex
	notes []string
}

func (f *fakePlat) Protect(b []byte) ([]byte, error)   { return append([]byte("x"), b...), nil }
func (f *fakePlat) Unprotect(b []byte) ([]byte, error) { return b[1:], nil }
func (f *fakePlat) Notify(t, b string) {
	f.mu.Lock()
	f.notes = append(f.notes, t+"|"+b)
	f.mu.Unlock()
}
func (f *fakePlat) OpenURL(string) error    { return nil }
func (f *fakePlat) OpenPath(string) error   { return nil }
func (f *fakePlat) SetAutostart(bool) error { return nil }
func (f *fakePlat) Autostart() bool         { return false }
func (f *fakePlat) SetTray(string, int)     {}
func (f *fakePlat) count() int              { f.mu.Lock(); defer f.mu.Unlock(); return len(f.notes) }
func (f *fakePlat) last() string            { f.mu.Lock(); defer f.mu.Unlock(); return f.notes[len(f.notes)-1] }

func issue(id int, status string, upd time.Time, notes ...mantis.Note) mantis.Issue {
	return mantis.Issue{ID: id, Summary: "issue", Status: mantis.Ref{Label: status}, Updated: upd, Notes: notes}
}

func waitNotes(t *testing.T, p *fakePlat, n int) {
	t.Helper()
	for i := 0; i < 100 && p.count() < n; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if p.count() != n {
		t.Fatalf("want %d notifications, got %d", n, p.count())
	}
}

func TestDiffAndNotifications(t *testing.T) {
	p := &fakePlat{}
	a := New(t.TempDir(), p, "test")
	a.settings.BaseURL, a.settings.Username = "http://x/mantisbt", "demo"
	t0 := time.Now().Add(-time.Hour)

	// 1st sync = baseline: nothing new, no notification.
	a.applyIssues([]mantis.Issue{issue(1, "已分配", t0), issue(2, "進行中", t0)})
	s := a.Snapshot()
	if s.NewCount != 0 || s.UpdatedCount != 0 {
		t.Fatalf("baseline should be read: %+v", s)
	}

	// New issue appears + issue 1 gets a note from someone else.
	t1 := t0.Add(10 * time.Minute)
	a.applyIssues([]mantis.Issue{
		issue(1, "待CR", t1, mantis.Note{Reporter: mantis.User{Name: "alice", RealName: "Alice"}, Created: t1}),
		issue(2, "進行中", t0),
		issue(3, "已分配", t1),
	})
	waitNotes(t, p, 1)
	if !strings.Contains(p.last(), "1 筆新項目、1 筆有更新") || !strings.Contains(p.last(), "狀態 已分配 → 待CR") {
		t.Fatalf("unexpected notification %q", p.last())
	}
	s = a.Snapshot()
	if s.NewCount != 1 || s.UpdatedCount != 1 {
		t.Fatalf("want 1 new 1 updated: %d %d", s.NewCount, s.UpdatedCount)
	}

	// Same data again: no duplicate notification.
	a.applyIssues([]mantis.Issue{
		issue(1, "待CR", t1, mantis.Note{Reporter: mantis.User{Name: "alice"}, Created: t1}),
		issue(2, "進行中", t0), issue(3, "已分配", t1),
	})
	time.Sleep(50 * time.Millisecond)
	if p.count() != 1 {
		t.Fatalf("duplicate notification")
	}

	// My own note (status unchanged) should not count as an update.
	t2 := t0.Add(20 * time.Minute)
	a.applyIssues([]mantis.Issue{
		issue(1, "待CR", t1, mantis.Note{Reporter: mantis.User{Name: "alice"}, Created: t1}),
		issue(2, "進行中", t2, mantis.Note{Reporter: mantis.User{Name: "demo"}, Created: t2}),
		issue(3, "已分配", t1),
	})
	time.Sleep(50 * time.Millisecond)
	if p.count() != 1 {
		t.Fatalf("own change notified: %v", p.notes)
	}

	a.MarkRead([]int{1, 3})
	s = a.Snapshot()
	if s.NewCount != 0 || s.UpdatedCount != 0 {
		t.Fatalf("mark read failed %d %d", s.NewCount, s.UpdatedCount)
	}

	// Issue 3 disappears and comes back -> it is new again.
	a.applyIssues([]mantis.Issue{issue(1, "待CR", t1), issue(2, "進行中", t2)})
	a.applyIssues([]mantis.Issue{issue(1, "待CR", t1), issue(2, "進行中", t2), issue(3, "已分配", t1)})
	waitNotes(t, p, 2)

	// State survives restart.
	b := New(a.dir, p, "test")
	b.settings = a.settings
	b.issues = a.issues
	if got := b.Snapshot().NewCount; got != 1 {
		t.Fatalf("persisted new count = %d", got)
	}
}

func TestStatusGroup(t *testing.T) {
	cases := map[string]mantis.Ref{
		"progress": {ID: 20, Name: "feedback", Label: "回饋"},
		"assigned": {ID: 50, Label: "已分配"},
		"review":   {ID: 60, Name: "review", Label: "待CR"},
		"done":     {ID: 80, Name: "resolved"},
		"testing":  {ID: 30, Label: "測試中"},
		"other":    {ID: 10, Name: "new", Label: "新建"},
	}
	for want, ref := range cases {
		if got := statusGroup(ref); got != want {
			t.Errorf("%+v => %s want %s", ref, got, want)
		}
	}
}
