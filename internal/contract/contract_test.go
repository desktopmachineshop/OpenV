// Package contract holds refactor plan step X4a's generator: the Go
// vocabularies the frontend reads, written from their Go catalogues into two
// checked-in files, frontend/src/generated/contract.ts (exported constants
// with the union types derived from them) and
// internal/contract/testdata/contract.json (the same values as JSON). Like
// internal/vocabparity (S13), the package has only test files: it imports
// the domain packages whose catalogues are exported and parses the Go
// sources for those that are not, by declaration name, so it adds no
// import edge (internal/archtest/ratchets.json) and nothing imports it.
//
// TestContract fails when either file differs from what the catalogues give
// today, and only UPDATE_CONTRACTS=1 rewrites them; any other value
// compares. TestContractAgreesWithS13 holds every vocabulary that
// contracts/vocab.json also lists equal to it, so the generated contract and
// S13's golden cannot drift apart. Refactor plan X4b moves the frontend's
// hand-written copies onto contract.ts; until then nothing imports it.
//
// Refactor plan X5 adds a third file, frontend/src/generated/wire.ts: one
// TypeScript interface per Go type the API sends most, written by reflect
// with encoding/json's rules and checked against json.Marshal of a sample
// (wire_test.go). TestWire holds it as TestContract holds the other two,
// under the same UPDATE_CONTRACTS=1, and frontend/src/api/wireCompat.ts
// holds the hand-written interfaces to it, by type only.
package contract

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

const (
	tsFile   = "frontend/src/generated/contract.ts"
	jsonFile = "internal/contract/testdata/contract.json"
)

// updateEnv set to exactly 1 rewrites both files instead of comparing.
const updateEnv = "UPDATE_CONTRACTS"

const regenerate = updateEnv + "=1 go test ./internal/contract/..."

// TestContract builds the contract from the Go catalogues and compares both
// generated files with it, byte for byte.
func TestContract(t *testing.T) {
	src := loadSources(t)
	c := buildContract(t, src)
	const why = "The frontend reads these values, and stored data, saved automations and deployed clients use " +
		"them, so a refactor never changes this file (it is on the refactor guard's golden list)."
	const after = "and contracts/vocab.json with S13's command if TestContractAgreesWithS13 asks for it."
	syncGenerated(t, src, []generated{
		{path: jsonFile, data: c.jsonBytes(t), from: "the Go catalogues", why: why, after: after},
		{path: tsFile, data: c.typescript(), from: "the Go catalogues", why: why, after: after},
	})
}

// generated is one file the package writes: its module-relative path, the
// bytes it must hold, and what a stale copy's message says: what it is
// generated from, why it must not drift, and what else to regenerate.
type generated struct {
	path, from, why, after string
	data                   []byte
}

// syncGenerated compares each file with the bytes the Go sources give now,
// or, under UPDATE_CONTRACTS=1, writes those that differ. Every generated
// file of the package goes through it, so one command regenerates them all.
func syncGenerated(t *testing.T, src *sources, files []generated) {
	t.Helper()
	if os.Getenv(updateEnv) == "1" {
		for _, f := range files {
			path := src.path(f.path)
			if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, f.data) {
				continue
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, f.data, 0o644); err != nil {
				t.Fatalf("write %s: %v", f.path, err)
			}
			t.Logf("regenerated %s", f.path)
		}
		return
	}
	for _, f := range files {
		want, err := os.ReadFile(src.path(f.path))
		if err != nil {
			t.Errorf("read %s: %v\nCreate it with:\n  %s", f.path, err, regenerate)
			continue
		}
		if bytes.Equal(want, f.data) {
			continue
		}
		t.Errorf("%s is stale: it does not match %s; first differences (- checked in, + generated now, by "+
			"line):\n%s\n%s If the change is deliberate, regenerate the generated files with:\n  %s\n(only %s=1 "+
			"regenerates; any other value compares), %s",
			f.path, f.from, strings.Join(lineDiff(string(want), string(f.data), 20), "\n"), f.why, regenerate,
			updateEnv, f.after)
	}
}

// fromS6 lists the vocabularies contract.json holds that contracts/vocab.json
// does not: each comes from another golden, which buildContract reads.
var fromS6 = map[string]string{"sse_events": sseContractFile}

// TestContractAgreesWithS13 compares every vocabulary of the contract that
// S13's contracts/vocab.json also holds with it, value for value and in
// order, and requires every other one to be accounted for in fromS6. The
// two are read from the same Go catalogues by two readers, so a difference
// means one of them reads its catalogue wrongly, or one golden is stale.
func TestContractAgreesWithS13(t *testing.T) {
	src := loadSources(t)
	mine := asJSONObject(t, "the contract", buildContract(t, src).jsonBytes(t))
	data, err := os.ReadFile(src.path("contracts/vocab.json"))
	if err != nil {
		t.Fatal(err)
	}
	vocab := asJSONObject(t, "contracts/vocab.json", data)

	keys := make([]string, 0, len(mine))
	for k := range mine {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	shared := 0
	for _, k := range keys {
		if k == "about" {
			continue
		}
		if golden, ok := fromS6[k]; ok {
			if _, inVocab := vocab[k]; inVocab {
				t.Errorf("%s: contracts/vocab.json now holds it too; compare it there and drop it from fromS6 (%s)",
					k, golden)
			}
			continue
		}
		want, ok := vocab[k]
		if !ok {
			t.Errorf("%s: contracts/vocab.json has no such vocabulary; name its golden in fromS6", k)
			continue
		}
		shared++
		if !reflect.DeepEqual(mine[k], want) {
			got, _ := json.Marshal(mine[k])
			pinned, _ := json.Marshal(want)
			t.Errorf("%s differs from contracts/vocab.json:\n  contract:   %s\n  vocab.json: %s\n"+
				"Run go test ./internal/vocabparity: if TestVocabulary passes, this package reads the catalogue "+
				"differently from S13 and its reader is wrong; if it fails, regenerate vocab.json as it says.",
				k, got, pinned)
		}
	}
	if shared == 0 {
		t.Error("no vocabulary in common with contracts/vocab.json: the comparison checks nothing")
	}
}

func asJSONObject(t *testing.T, name string, data []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return out
}
