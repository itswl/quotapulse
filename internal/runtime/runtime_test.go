package runtime

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"quotapulse/internal/core"
	"quotapulse/internal/engine"
	"quotapulse/internal/secret"
	"quotapulse/internal/store"
)

type fixture struct {
	runner *Runner
	engine *engine.Engine
	dir    string
	now    time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "quotapulse.db"), secret.FromPassphrase("test"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	f := &fixture{dir: dir, now: time.Date(2026, 11, 16, 2, 0, 0, 0, time.UTC)}
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	f.engine = engine.New(engine.Options{Store: st, Log: quiet, Now: func() time.Time { return f.now }})
	f.runner = New(f.engine, quiet, dir)
	return f
}

func backups(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, "backups"))
	if err != nil {
		return nil
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func TestBackupIsAReadableDatabaseAndOldOnesAreRotatedOut(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	one := 1.0
	if _, err := f.engine.Ingest(ctx, []engine.Push{{Meter: "m", Kind: core.Balance, Value: &one}}); err != nil {
		t.Fatal(err)
	}

	path, err := f.runner.Backup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "quotapulse-20261116.db" {
		t.Errorf("backup name = %s", path)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("backup file: %v %v", info, err)
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var readings int
	if err := db.QueryRow("SELECT COUNT(*) FROM readings").Scan(&readings); err != nil || readings != 1 {
		t.Fatalf("the backup holds %d readings: %v", readings, err)
	}

	// Ten days of backups: only the newest seven stay.
	for day := 1; day <= 9; day++ {
		f.now = f.now.Add(24 * time.Hour)
		if _, err := f.runner.Backup(ctx); err != nil {
			t.Fatal(err)
		}
	}
	names := backups(t, f.dir)
	if len(names) != backupsKept || names[0] != "quotapulse-20261119.db" || names[len(names)-1] != "quotapulse-20261125.db" {
		t.Fatalf("kept %v", names)
	}
	// Files that are not ours are left alone.
	stray := filepath.Join(f.dir, "backups", "notes.txt")
	if err := os.WriteFile(stray, []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(24 * time.Hour)
	if _, err := f.runner.Backup(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stray); err != nil {
		t.Error("a file that is not a backup was removed")
	}
}

func TestDailyChoresRunOnceADay(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for range 3 {
		if err := f.runner.dailyChores(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if names := backups(t, f.dir); len(names) != 1 {
		t.Fatalf("three runs on one day made %v", names)
	}
	f.now = f.now.Add(24 * time.Hour)
	if err := f.runner.dailyChores(ctx); err != nil {
		t.Fatal(err)
	}
	if names := backups(t, f.dir); len(names) != 2 {
		t.Fatalf("the next day made %v", names)
	}
}

func TestNoDataDirectoryMeansNoBackups(t *testing.T) {
	f := newFixture(t)
	f.runner.dataDir = ""
	if path, err := f.runner.Backup(context.Background()); err != nil || path != "" {
		t.Fatalf("backup = %q, %v", path, err)
	}
}

func TestHealthFollowsTheLoops(t *testing.T) {
	f := newFixture(t)
	now := time.Now()
	if ok, why := f.runner.Healthy(now); !ok {
		t.Fatalf("before the loops have started: %s", why)
	}
	f.runner.record("deliver", nil)
	f.runner.record("calendar", nil)
	if ok, why := f.runner.Healthy(now); !ok {
		t.Fatalf("just after running: %s", why)
	}
	// A loop that has not come round for an hour has stopped.
	if ok, why := f.runner.Healthy(now.Add(time.Hour)); ok || why == "" {
		t.Fatal("a stalled loop went unnoticed")
	}
	jobs := f.runner.Jobs()
	if len(jobs) != 4 || jobs[0].Name != "calendar" || jobs[0].Runs != 1 || jobs[0].LastRun == nil {
		t.Fatalf("jobs = %+v", jobs)
	}
}

func TestALoopSurvivesAPanicAndRecordsIt(t *testing.T) {
	f := newFixture(t)
	f.runner.record("calendar", guard(func() error { panic("a bad reading") }))
	for _, job := range f.runner.Jobs() {
		if job.Name == "calendar" && job.LastError != "panic: a bad reading" {
			t.Errorf("job = %+v", job)
		}
	}
}

func TestTheLoopsDoTheirWork(t *testing.T) {
	f := newFixture(t)
	f.engine = nil
	// A real clock and real loops, for a moment: a pushed reading that opens
	// an incident must end up evaluated and the loops recorded as running.
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "quotapulse.db"), secret.FromPassphrase("test"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	e := engine.New(engine.Options{Store: st, Log: quiet})
	runner := New(e, quiet, dir)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runner.Run(ctx)
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		ran := 0
		for _, job := range runner.Jobs() {
			if job.Runs > 0 && job.LastError == "" {
				ran++
			}
		}
		if ran == 4 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not every loop ran: %+v", runner.Jobs())
		}
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the loops did not stop when asked")
	}
	if names := backups(t, dir); len(names) != 1 {
		t.Errorf("the first housekeeping pass should have written a backup: %v", names)
	}
}
