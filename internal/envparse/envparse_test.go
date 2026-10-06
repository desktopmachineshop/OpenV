package envparse

import (
	"bytes"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"
)

// captureLog sends slog's default logger to a buffer for the test, and
// forgets every warning already given, so each test sees its own.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	})))
	warned.Clear()
	t.Cleanup(func() {
		slog.SetDefault(prev)
		warned.Clear()
	})
	return &buf
}

func TestCountIsAWholeNumberAboveZero(t *testing.T) {
	cases := map[string]struct {
		want  int
		warns bool
	}{
		"":                     {42, false},
		"   ":                  {42, false},
		"7":                    {7, false},
		" 7 ":                  {7, false},
		"+7":                   {7, false},
		"007":                  {7, false},
		"0":                    {42, true},
		"-5":                   {42, true},
		"7x":                   {42, true},
		"30d":                  {42, true},
		"1e3":                  {42, true},
		"2.5":                  {42, true},
		"Inf":                  {42, true},
		"TRUE":                 {42, true},
		"99999999999999999999": {42, true},
	}
	for raw, c := range cases {
		log := captureLog(t)
		if got := Count("OPENV_TEST_COUNT", raw, 42); got != c.want {
			t.Errorf("Count(%q) = %d, want %d", raw, got, c.want)
		}
		checkWarning(t, log.String(), raw, "OPENV_TEST_COUNT", c.warns)
	}
}

func TestNumberTakesAnySign(t *testing.T) {
	cases := map[string]struct {
		want  int
		warns bool
	}{
		"":     {1024, false},
		" 0 ":  {0, false},
		"-1":   {-1, false},
		"2048": {2048, false},
		"2k":   {1024, true},
		"1e3":  {1024, true},
	}
	for raw, c := range cases {
		log := captureLog(t)
		if got := Number("OPENV_TEST_NUMBER", raw, 1024); got != c.want {
			t.Errorf("Number(%q) = %d, want %d", raw, got, c.want)
		}
		checkWarning(t, log.String(), raw, "OPENV_TEST_NUMBER", c.warns)
	}
}

func TestDurationIsPositive(t *testing.T) {
	def := 24 * time.Hour
	cases := map[string]struct {
		want  time.Duration
		warns bool
	}{
		"":      {def, false},
		"  ":    {def, false},
		"90m":   {90 * time.Minute, false},
		" 2h ":  {2 * time.Hour, false},
		"1h30m": {90 * time.Minute, false},
		"0":     {def, true},
		"0s":    {def, true},
		"-1h":   {def, true},
		"7":     {def, true},
		"30d":   {def, true},
		"TRUE":  {def, true},
	}
	for raw, c := range cases {
		log := captureLog(t)
		if got := Duration("OPENV_TEST_DURATION", raw, def); got != c.want {
			t.Errorf("Duration(%q) = %s, want %s", raw, got, c.want)
		}
		checkWarning(t, log.String(), raw, "OPENV_TEST_DURATION", c.warns)
	}
}

func TestRateIsPositiveAndFinite(t *testing.T) {
	cases := map[string]struct {
		want  float64
		warns bool
	}{
		"":         {120, false},
		"60":       {60, false},
		" 7 ":      {7, false},
		"2.5":      {2.5, false},
		"1e3":      {1000, false},
		"0":        {120, true},
		"-5":       {120, true},
		"Inf":      {120, true},
		"+Inf":     {120, true},
		"Infinity": {120, true},
		"NaN":      {120, true},
		"1e400":    {120, true},
		"20/h":     {120, true},
		"a, ,b":    {120, true},
	}
	for raw, c := range cases {
		log := captureLog(t)
		got := Rate("OPENV_TEST_REFILL_PER_HOUR", raw, 120)
		if got != c.want || math.IsInf(got, 0) || math.IsNaN(got) {
			t.Errorf("Rate(%q) = %v, want %v", raw, got, c.want)
		}
		checkWarning(t, log.String(), raw, "OPENV_TEST_REFILL_PER_HOUR", c.warns)
	}
}

func TestBoolTakesTrueFalseInAnyCaseAndOneZero(t *testing.T) {
	cases := []struct {
		raw   string
		def   bool
		want  bool
		warns bool
	}{
		{"", false, false, false},
		{"", true, true, false},
		{"  ", true, true, false},
		{"true", false, true, false},
		{"TRUE", false, true, false},
		{" True ", false, true, false},
		{"1", false, true, false},
		{"false", true, false, false},
		{"FALSE", true, false, false},
		{"0", true, false, false},
		{"yes", false, false, true},
		{"on", true, true, true},
		{"t", false, false, true},
		{"2", false, false, true},
	}
	for _, c := range cases {
		log := captureLog(t)
		if got := Bool("OPENV_TEST_BOOL", c.raw, c.def); got != c.want {
			t.Errorf("Bool(%q, default %v) = %v, want %v", c.raw, c.def, got, c.want)
		}
		checkWarning(t, log.String(), c.raw, "OPENV_TEST_BOOL", c.warns)
	}
}

// A switch takes on or off in any case as well as a boolean's spellings
// (OPENV_UPLOAD_SWEEP=off, #379 question 55); anything else keeps the
// default with the one warning.
func TestSwitchTakesOnOffAsWellAsABoolean(t *testing.T) {
	cases := []struct {
		raw   string
		def   bool
		want  bool
		warns bool
	}{
		{"", true, true, false},
		{"  ", false, false, false},
		{"off", true, false, false},
		{" OFF ", true, false, false},
		{"Off", true, false, false},
		{"on", false, true, false},
		{"ON", false, true, false},
		{"false", true, false, false},
		{"0", true, false, false},
		{"true", false, true, false},
		{"1", false, true, false},
		{"no", true, true, true},
		{"of", true, true, true},
		{"disabled", true, true, true},
		{"o n", false, false, true},
	}
	for _, c := range cases {
		log := captureLog(t)
		if got := Switch("OPENV_TEST_SWITCH", c.raw, c.def); got != c.want {
			t.Errorf("Switch(%q, default %v) = %v, want %v", c.raw, c.def, got, c.want)
		}
		checkWarning(t, log.String(), c.raw, "OPENV_TEST_SWITCH", c.warns)
	}
}

// A size in mebibytes keeps its default when its bytes would not fit in an
// int64 (#379, bug 224): 8796093022208 MiB is 2^63 bytes, which wraps round
// to a negative cap. The warning names the variable, never the value, and
// comes once however often the size is read.
func TestMebibytesFitInAnInt64(t *testing.T) {
	if want := strconv.FormatInt(MaxMebibytes, 10); !strings.HasSuffix(wantMebibytes, " "+want) {
		t.Fatalf("the warning says %q, but MaxMebibytes is %s", wantMebibytes, want)
	}
	if most := MaxMebibytes; most<<20 < 0 || (most+1)<<20 >= 0 {
		t.Fatalf("MaxMebibytes = %d is not the largest count of mebibytes an int64 holds in bytes", MaxMebibytes)
	}
	cases := []struct {
		n, want int
		warns   bool
	}{
		{1, 1, false},
		{32, 32, false},
		{int(MaxMebibytes), int(MaxMebibytes), false},
		{int(MaxMebibytes) + 1, 32, true},
		{math.MaxInt, 32, true},
	}
	for _, c := range cases {
		log := captureLog(t)
		for range 3 {
			if got := Mebibytes("OPENV_TEST_MB", c.n, 32); got != c.want {
				t.Errorf("Mebibytes(%d) = %d, want %d", c.n, got, c.want)
			}
		}
		checkWarning(t, log.String(), strconv.Itoa(c.n), "OPENV_TEST_MB", c.warns)
		if c.warns && strings.Contains(log.String(), strconv.Itoa(c.n)) {
			t.Errorf("Mebibytes(%d): the warning printed the value:\n%s", c.n, log)
		}
	}
}

// An off switch takes off in any case and nothing else (#379, bug 225):
// false and 0 are not off, and anything but off or a blank value keeps what
// it controls on, with one warning naming the variable and what it takes.
func TestOffTakesOnlyOff(t *testing.T) {
	cases := []struct {
		raw   string
		want  bool
		warns bool
	}{
		{"", false, false},
		{"   ", false, false},
		{"off", true, false},
		{" OFF ", true, false},
		{"Off\n", true, false},
		{"false", false, true},
		{"0", false, true},
		{"on", false, true},
		{"no", false, true},
		{"of", false, true},
		{"o ff", false, true},
	}
	for _, c := range cases {
		log := captureLog(t)
		for range 3 {
			if got := Off("OPENV_TEST_OFF", c.raw); got != c.want {
				t.Errorf("Off(%q) = %v, want %v", c.raw, got, c.want)
			}
		}
		checkWarning(t, log.String(), c.raw, "OPENV_TEST_OFF", c.warns)
		if c.warns && !strings.Contains(log.String(), `want="off (any case), or unset"`) {
			t.Errorf("Off(%q): the warning does not say what the setting takes:\n%s", c.raw, log)
		}
		if c.warns && strings.Contains(log.String(), "="+strings.TrimSpace(c.raw)+" ") {
			t.Errorf("Off(%q): the warning printed the value:\n%s", c.raw, log)
		}
	}
}

func TestTextIsTrimmed(t *testing.T) {
	for raw, want := range map[string]string{"": "def", "   ": "def", "x": "x", " x ": "x", "a b": "a b", "\tx\n": "x"} {
		if got := Text(raw, "def"); got != want {
			t.Errorf("Text(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestWholeNumberRefusesTrailingJunk(t *testing.T) {
	for raw, want := range map[string]int{"7": 7, " 7 ": 7, "+7": 7, "0": 0, "-5": -5} {
		if got, ok := WholeNumber(raw); !ok || got != want {
			t.Errorf("WholeNumber(%q) = %d, %v; want %d, true", raw, got, ok, want)
		}
	}
	for _, raw := range []string{"", "  ", "7x", "30d", "1e3", "2.5", "Inf", "0x10", "7 8"} {
		if got, ok := WholeNumber(raw); ok {
			t.Errorf("WholeNumber(%q) = %d, true; want false", raw, got)
		}
	}
}

// TestAMalformedSettingWarnsOnce reads the same malformed value three times,
// as the server does with a setting it reads twice at boot or on every
// request: one warning, naming the variable and not the value. Another
// variable, or another value, warns again.
func TestAMalformedSettingWarnsOnce(t *testing.T) {
	log := captureLog(t)
	for range 3 {
		Bool("SECURE_COOKIES", "sk_live_pasted_by_mistake", false)
	}
	Count("OPENV_SHARED_PRODUCT_DAILY_LIMIT", "20x", 20)
	Bool("SECURE_COOKIES", "yes", false)
	got := strings.Split(strings.TrimSpace(log.String()), "\n")
	want := []string{
		`level=WARN msg="ignoring a malformed setting; its default applies" var=SECURE_COOKIES want="true or false (any case), or 1 or 0"`,
		`level=WARN msg="ignoring a malformed setting; its default applies" var=OPENV_SHARED_PRODUCT_DAILY_LIMIT want="a whole number above 0"`,
		`level=WARN msg="ignoring a malformed setting; its default applies" var=SECURE_COOKIES want="true or false (any case), or 1 or 0"`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("log:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if strings.Contains(log.String(), "sk_live") {
		t.Errorf("a warning printed the value:\n%s", log.String())
	}
}

// checkWarning checks that reading raw logged one warning naming name, and
// nothing else, or nothing at all. TestAMalformedSettingWarnsOnce holds the
// warning's whole line, which carries no value.
func checkWarning(t *testing.T, log, raw, name string, want bool) {
	t.Helper()
	switch {
	case want && (strings.Count(log, "\n") != 1 || !strings.Contains(log, "var="+name+" ")):
		t.Errorf("%q: want one warning naming %s, got:\n%s", raw, name, log)
	case !want && log != "":
		t.Errorf("%q: want no warning, got:\n%s", raw, log)
	}
}

// TestSecretIsUsedExactlyAsSet: a credential is never trimmed, by the
// maintainer's decision on #379's question 24, since a key cut short of its
// spaces is another key. One with spaces or a line break around it, or
// only those, warns once, naming the variable and never the value; spaces
// inside it are part of it and warn nothing.
func TestSecretIsUsedExactlyAsSet(t *testing.T) {
	const line = `level=WARN msg="a credential setting has spaces or a line break around it; it is used exactly as set" var=WORKER_API_KEY`
	cases := map[string]bool{
		"":                          false,
		"wk-sk_live_do_not_log":     false,
		"wk sk_live do not log":     false,
		"wk-sk_live_do_not_log\n":   true,
		" wk-sk_live_do_not_log":    true,
		"\twk-sk_live_do_not_log\r": true,
		"   ":                       true,
		"\n":                        true,
	}
	for raw, warns := range cases {
		log := captureLog(t)
		if got := Secret("WORKER_API_KEY", raw); got != raw {
			t.Errorf("Secret(%q) = %q, want it exactly as set", raw, got)
		}
		got := strings.TrimSuffix(log.String(), "\n")
		switch {
		case warns && got != line:
			t.Errorf("Secret(%q) logged:\n%s\nwant one warning:\n%s", raw, got, line)
		case !warns && got != "":
			t.Errorf("Secret(%q) logged:\n%s\nwant nothing", raw, got)
		}
		if strings.Contains(log.String(), "sk_live") {
			t.Errorf("Secret(%q) printed the value:\n%s", raw, log.String())
		}
	}
}

// TestASecretWithSpacesWarnsOnce reads the same credential three times, as
// the server and agentd might: one warning. Another variable warns again.
// What remembers the warnings holds a digest of the value, not the value.
func TestASecretWithSpacesWarnsOnce(t *testing.T) {
	log := captureLog(t)
	for range 3 {
		Secret("RUNNER_POOL_KEY", "pk-sk_live_do_not_log\n")
	}
	Secret("DB_PASSWORD", " sk_live_do_not_log")
	want := `level=WARN msg="a credential setting has spaces or a line break around it; it is used exactly as set" var=RUNNER_POOL_KEY
level=WARN msg="a credential setting has spaces or a line break around it; it is used exactly as set" var=DB_PASSWORD`
	if got := strings.TrimSpace(log.String()); got != want {
		t.Errorf("log:\n%s\nwant:\n%s", got, want)
	}
	warned.Range(func(k, _ any) bool {
		if strings.Contains(k.(string), "sk_live") {
			t.Errorf("the warnings remember the value itself: %q", k)
		}
		return true
	})
}
