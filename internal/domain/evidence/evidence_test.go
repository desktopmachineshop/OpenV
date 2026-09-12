package evidence

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateRequiresATitleAndTrimsIt(t *testing.T) {
	if _, _, _, _, err := Validate("   ", "", "", nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a whitespace title was accepted: %v", err)
	}
	title, summary, by, _, err := Validate("  Noise sweep  ", "  ran overnight  ", "  J. Patel  ", nil)
	if err != nil {
		t.Fatalf("a valid bundle was refused: %v", err)
	}
	if title != "Noise sweep" || summary != "ran overnight" || by != "J. Patel" {
		t.Fatalf("fields were not trimmed: %q / %q / %q", title, summary, by)
	}
}

// A bundle with no files and only a written account is the inspection and
// demonstration case, and must be valid.
func TestValidateAcceptsAWrittenOnlyBundle(t *testing.T) {
	if _, _, _, _, err := Validate("Visual inspection of weld seam", "No porosity visible under 10x.", "", nil); err != nil {
		t.Fatalf("a file-less bundle was refused: %v", err)
	}
}

func TestValidateBoundsEveryUserSuppliedField(t *testing.T) {
	long := strings.Repeat("x", MaxTitleLen+1)
	if _, _, _, _, err := Validate(long, "", "", nil); !errors.Is(err, ErrInvalid) {
		t.Error("an over-long title was accepted")
	}
	if _, _, _, _, err := Validate("ok", strings.Repeat("x", MaxSummaryLen+1), "", nil); !errors.Is(err, ErrInvalid) {
		t.Error("an over-long summary was accepted")
	}
	if _, _, _, _, err := Validate("ok", "", strings.Repeat("x", MaxCapturedByLen+1), nil); !errors.Is(err, ErrInvalid) {
		t.Error("an over-long captured-by was accepted")
	}

	tooMany := map[string]interface{}{}
	for i := 0; i <= MaxConditionKeys; i++ {
		tooMany[strings.Repeat("k", i+1)] = 1
	}
	if _, _, _, _, err := Validate("ok", "", "", tooMany); !errors.Is(err, ErrInvalid) {
		t.Error("an unbounded conditions map was accepted")
	}
	if _, _, _, _, err := Validate("ok", "", "", map[string]interface{}{
		"rig": strings.Repeat("x", MaxConditionValueLen+1),
	}); !errors.Is(err, ErrInvalid) {
		t.Error("an over-long condition value was accepted")
	}
}

// Conditions are open-ended, so the only shaping is trimming and dropping
// blank keys — a non-string value passes through as itself.
func TestValidateKeepsConditionTypesAndDropsBlankKeys(t *testing.T) {
	_, _, _, conditions, err := Validate("ok", "", "", map[string]interface{}{
		"rig":       "  chamber 2  ",
		"ambient_c": 21.5,
		"   ":       "dropped",
	})
	if err != nil {
		t.Fatal(err)
	}
	if conditions["rig"] != "chamber 2" {
		t.Errorf("string condition not trimmed: %q", conditions["rig"])
	}
	if conditions["ambient_c"] != 21.5 {
		t.Errorf("numeric condition was altered: %v", conditions["ambient_c"])
	}
	if len(conditions) != 2 {
		t.Errorf("blank key survived: %+v", conditions)
	}
}

func TestHumanBytesReadsLikeAPersonWroteIt(t *testing.T) {
	for _, tc := range []struct {
		in   int64
		want string
	}{
		{512, "512 B"},
		{2048, "2.0 KB"},
		{5 * 1024 * 1024, "5.0 MB"},
		{3 * 1024 * 1024 * 1024, "3.0 GB"},
	} {
		if got := HumanBytes(tc.in); got != tc.want {
			t.Errorf("HumanBytes(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
