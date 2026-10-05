package snapshot

import (
	"encoding/json"
	"time"
)

// Baseline is a stored baseline as Load reads it: what names it, and the
// snapshot JSON it keeps.
type Baseline struct {
	ID        string
	Name      string
	CreatedAt time.Time
	Snapshot  []byte
}

// Options are where Load reads a snapshot. This package imports no service,
// so its caller supplies both reads.
type Options struct {
	// Baseline reads a baseline of the project, scoped to it: one that is
	// missing, malformed or another project's is an error, which Load
	// returns as it is (baselines.ErrNotFound from the baseline service).
	Baseline func(projectID, baselineID string) (*Baseline, error)
	// Live renders the live project as its JSON export.
	Live func(projectID string) ([]byte, error)
}

// DecodeError is a snapshot whose JSON does not decode as a ProjectExport.
// It reads as encoding/json's error, which it wraps, so a caller that logs
// it logs what a decode of its own would.
type DecodeError struct {
	Err error
}

func (e *DecodeError) Error() string { return e.Err.Error() }

func (e *DecodeError) Unwrap() error { return e.Err }

// Load is the one way to read a project's snapshot as a ProjectExport: the
// baseline baselineID names, or the live project when baselineID is empty.
// Either way it decodes JSON, the baseline's stored snapshot or the live
// JSON export, so a live read and a baseline read are the same document.
// It returns the baseline it read, nil for the live project. An error
// reading either is returned as it is, and JSON that does not decode is a
// *DecodeError, so each caller answers each as it always has.
func Load(projectID, baselineID string, opts Options) (*ProjectExport, *Baseline, error) {
	var (
		raw  []byte
		from *Baseline
	)
	if baselineID != "" {
		b, err := opts.Baseline(projectID, baselineID)
		if err != nil {
			return nil, nil, err
		}
		raw, from = b.Snapshot, b
	} else {
		live, err := opts.Live(projectID)
		if err != nil {
			return nil, nil, err
		}
		raw = live
	}
	var data ProjectExport
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, from, &DecodeError{Err: err}
	}
	return &data, from, nil
}
