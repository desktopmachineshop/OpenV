package postgres

import (
	"database/sql"
	"errors"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// secondPool opens another connection pool to db's database, as a second
// server process booting on it would.
func secondPool(t *testing.T, db *sql.DB) *sql.DB {
	t.Helper()
	var name string
	if err := db.QueryRow(`SELECT current_database()`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(os.Getenv(TestDatabaseURLEnv))
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	other, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = other.Close() })
	return other
}

// A boot task runs once per database (#379 question 48): of two processes
// booting at once, one runs it while the other waits on the boot lock and
// then finds it recorded; a later boot finds it recorded too. A task that
// fails is not recorded, so the next boot runs it again.
func TestABootTaskRunsOncePerDatabase(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	other := secondPool(t, db)

	var runs atomic.Int32
	task := func() (string, error) {
		runs.Add(1)
		time.Sleep(300 * time.Millisecond)
		return "swept", nil
	}
	var wg sync.WaitGroup
	ran := make([]bool, 2)
	errs := make([]error, 2)
	for i, pool := range []*sql.DB{db, other} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ran[i], errs[i] = RunBootTaskOnce(pool, "a task", task)
		}()
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("RunBootTaskOnce: %v, %v", errs[0], errs[1])
	}
	if runs.Load() != 1 || ran[0] == ran[1] {
		t.Errorf("two processes booting at once ran the task %d times (ran: %v), want once", runs.Load(), ran)
	}
	var outcome string
	if err := db.QueryRow(`SELECT outcome FROM boot_tasks WHERE name = 'a task'`).Scan(&outcome); err != nil || outcome != "swept" {
		t.Errorf("the task's record: %q, %v; want its outcome", outcome, err)
	}
	if again, err := RunBootTaskOnce(db, "a task", task); err != nil || again || runs.Load() != 1 {
		t.Errorf("a later boot: ran %v, %v (runs %d); want the task skipped", again, err, runs.Load())
	}

	failure := errors.New("the uploads directory could not be listed")
	if ran, err := RunBootTaskOnce(db, "a failing task", func() (string, error) { return "", failure }); !ran || !errors.Is(err, failure) {
		t.Errorf("a failing task: ran %v, %v; want it run and its error", ran, err)
	}
	if ran, err := RunBootTaskOnce(db, "a failing task", func() (string, error) { return "done", nil }); !ran || err != nil {
		t.Errorf("the next boot after a failing task: ran %v, %v; want it run again", ran, err)
	}
}
