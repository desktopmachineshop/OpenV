package snapshot

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// TestLoad pins what each caller of Load relies on to answer as it did
// before X14b: an empty baselineID reads the live JSON export and any other,
// "live" too, reads that baseline, scoped to the project; the baseline read
// comes back beside the snapshot; an error reading either comes back as it
// is; and JSON that does not decode is a *DecodeError that reads as, and
// wraps, encoding/json's own error.
func TestLoad(t *testing.T) {
	stored := &Baseline{ID: "b1", Name: "v0.2.0", CreatedAt: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
		Snapshot: []byte(`{"project_name":"At v0.2.0","artifacts":[{"id":"r1","title":"Old"}]}`)}
	broken := &Baseline{ID: "b2", Name: "broken", Snapshot: []byte(`{"artifacts":`)}
	errMissing := errors.New("baseline not found")
	errExport := errors.New("failed to get project")
	var reads []string
	opts := Options{
		Baseline: func(projectID, baselineID string) (*Baseline, error) {
			reads = append(reads, "baseline "+projectID+" "+baselineID)
			if projectID == "p1" && baselineID == stored.ID {
				return stored, nil
			}
			if projectID == "p1" && baselineID == broken.ID {
				return broken, nil
			}
			return nil, errMissing
		},
		Live: func(projectID string) ([]byte, error) {
			reads = append(reads, "live "+projectID)
			switch projectID {
			case "p1":
				return []byte(`{"project_name":"Now","artifacts":[]}`), nil
			case "p2":
				return []byte(`[]`), nil
			}
			return nil, errExport
		},
	}
	jsonError := func(raw string) string {
		var data ProjectExport
		return json.Unmarshal([]byte(raw), &data).Error()
	}

	for _, c := range []struct {
		name, project, baseline string
		read                    string
		title                   string // the snapshot's project name, when it loads
		from                    *Baseline
		err                     error  // the read's own error, returned as it is
		decode                  string // the text of the DecodeError
	}{
		{name: "live", project: "p1", read: "live p1", title: "Now"},
		{name: "baseline", project: "p1", baseline: "b1", read: "baseline p1 b1", title: "At v0.2.0", from: stored},
		{name: "another project's baseline", project: "p3", baseline: "b1", read: "baseline p3 b1", err: errMissing},
		{name: "live as a baseline id", project: "p1", baseline: "live", read: "baseline p1 live", err: errMissing},
		{name: "live export fails", project: "p9", read: "live p9", err: errExport},
		{name: "baseline does not decode", project: "p1", baseline: "b2", read: "baseline p1 b2", from: broken,
			decode: jsonError(`{"artifacts":`)},
		{name: "live export does not decode", project: "p2", read: "live p2", decode: jsonError(`[]`)},
	} {
		t.Run(c.name, func(t *testing.T) {
			reads = nil
			data, from, err := Load(c.project, c.baseline, opts)
			if len(reads) != 1 || reads[0] != c.read {
				t.Errorf("read %v, want [%s]", reads, c.read)
			}
			if from != c.from {
				t.Errorf("baseline read = %+v, want %+v", from, c.from)
			}
			var bad *DecodeError
			switch {
			case c.err != nil:
				if err != c.err || data != nil {
					t.Errorf("Load = %v, %v; want nil and the read's own error", data, err)
				}
			case c.decode != "":
				if !errors.As(err, &bad) || err.Error() != c.decode || data != nil {
					t.Errorf("Load = %v, %v (%T); want nil and a *DecodeError reading %q", data, err, err, c.decode)
				}
				var syntax *json.SyntaxError
				var wrongType *json.UnmarshalTypeError
				if !errors.As(err, &syntax) && !errors.As(err, &wrongType) {
					t.Errorf("the DecodeError does not wrap encoding/json's error: %#v", err)
				}
			default:
				if err != nil || data == nil || data.ProjectName != c.title {
					t.Fatalf("Load = %+v, %v; want the snapshot of %q", data, err, c.title)
				}
			}
		})
	}
	if data, _, _ := Load("p1", "b1", opts); len(data.Artifacts) != 1 || data.Artifacts[0].Title != "Old" {
		t.Errorf("the baseline's artifacts = %+v", data.Artifacts)
	}
}
