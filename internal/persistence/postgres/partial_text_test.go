package postgres

import (
	"testing"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/guided"
)

// partial_text follows the run's life: written while it is live, readable
// with the run, and gone once it finishes (final_text takes over).
func TestUpdatePartialText(t *testing.T) {
	f := newClaimFixture(t)
	runID := f.queueRun(t, runSpec{})

	t.Run("a queued run is not streaming yet", func(t *testing.T) {
		applied, err := f.repo.UpdatePartialText(runID, "too early")
		if err != nil {
			t.Fatal(err)
		}
		if applied {
			t.Fatal("partial text was written to a queued run")
		}
	})

	f.setRunState(t, runID, agentruns.StatusRunning, "token-hash")

	t.Run("a live run streams", func(t *testing.T) {
		applied, err := f.repo.UpdatePartialText(runID, "Your vision statement")
		if err != nil {
			t.Fatal(err)
		}
		if !applied {
			t.Fatal("partial text was not written to a running run")
		}
		if got := f.mustFind(t, runID).PartialText; got != "Your vision statement" {
			t.Fatalf("partial_text = %q", got)
		}
		// The whole text each time, not a delta.
		if _, err := f.repo.UpdatePartialText(runID, "Your vision statement is vague."); err != nil {
			t.Fatal(err)
		}
		if got := f.mustFind(t, runID).PartialText; got != "Your vision statement is vague." {
			t.Fatalf("partial_text after the second write = %q", got)
		}
	})

	t.Run("finishing clears it", func(t *testing.T) {
		run := f.mustFind(t, runID)
		run.Status = agentruns.StatusSucceeded
		run.FinalText = "Your vision statement is vague. Try naming the user."
		applied, err := f.repo.UpdateTerminal(run)
		if err != nil || !applied {
			t.Fatalf("UpdateTerminal = %v, %v", applied, err)
		}
		got := f.mustFind(t, runID)
		if got.PartialText != "" {
			t.Fatalf("partial_text survived the finish: %q", got.PartialText)
		}
		if got.FinalText == "" {
			t.Fatal("final text was lost")
		}
	})

	t.Run("a late batch cannot revive a finished run", func(t *testing.T) {
		applied, err := f.repo.UpdatePartialText(runID, "a straggling batch")
		if err != nil {
			t.Fatal(err)
		}
		if applied {
			t.Fatal("a terminal run accepted partial text")
		}
		if got := f.mustFind(t, runID).PartialText; got != "" {
			t.Fatalf("partial_text = %q on a finished run", got)
		}
	})
}

// A parked wizard nudge is read back once and only once: taking it clears it
// in the same statement, so two finishing runs cannot both answer it.
func TestPendingNudgeSetAndTake(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewGuidedRepository(db)

	orgID := uuid.New().String()
	projectID := uuid.New().String()
	if _, err := db.Exec(`INSERT INTO organizations (id, name, slug) VALUES ($1, 'Nudge Org', $2)`, orgID, "nudge-"+orgID[:8]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO projects (id, org_id, name) VALUES ($1, $2, 'Nudge Project')`, projectID, orgID); err != nil {
		t.Fatal(err)
	}
	session := &guided.Session{ID: uuid.New().String(), ProjectID: projectID, Status: guided.StatusInProgress, CurrentStep: 1}
	if err := repo.Save(session); err != nil {
		t.Fatal(err)
	}

	got, err := repo.TakePendingNudge(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("a fresh session has a parked nudge: %+v", got)
	}

	if err := repo.SetPendingNudge(session.ID, &guided.PendingNudge{Step: 2, Event: "saved step 2"}); err != nil {
		t.Fatal(err)
	}
	// The newest nudge overwrites the earlier one.
	if err := repo.SetPendingNudge(session.ID, &guided.PendingNudge{
		Step:  4,
		State: map[string]interface{}{"step_4": "requirements"},
		Event: "saved step 4",
	}); err != nil {
		t.Fatal(err)
	}

	stored, err := repo.FindByID(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.PendingNudge == nil || stored.PendingNudge.Step != 4 {
		t.Fatalf("session reads back %+v, want the newest nudge", stored.PendingNudge)
	}

	// An unrelated session update must not clobber the parked nudge.
	stored.CurrentStep = 5
	if err := repo.Update(stored); err != nil {
		t.Fatal(err)
	}

	got, err = repo.TakePendingNudge(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Step != 4 || got.Event != "saved step 4" || got.State["step_4"] != "requirements" {
		t.Fatalf("took %+v, want the newest nudge with its state", got)
	}

	again, err := repo.TakePendingNudge(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again != nil {
		t.Fatalf("the nudge was handed out twice: %+v", again)
	}
}
