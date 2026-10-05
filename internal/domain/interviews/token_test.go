package interviews

import (
	"crypto/rand"
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/users"
)

// Refactor step P2a (docs/plans/codebase-refactor.md, §6.6): interviews
// keeps its own copies of the token generator and hasher, generateToken and
// hashToken. These tests pin them with the tables users/token_test.go pins
// users.NewToken and users.HashToken with, and pin whether the copies give
// output identical to users': the same token from the same random bytes,
// and the same hash of the same input. P2b may move the copy to
// internal/domain/tokens only if they do.

// tokenFormat is the whole format of a minted token and of a token hash.
var tokenFormat = regexp.MustCompile(`^[0-9a-f]{64}$`)

// countingSource yields 0x00, 0x01, 0x02 and so on, so successive draws show
// how many random bytes each token takes and in which order it writes them.
type countingSource struct{ next byte }

func (s *countingSource) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = s.next
		s.next++
	}
	return len(p), nil
}

// constSource yields one byte value forever.
type constSource byte

func (s constSource) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(s)
	}
	return len(p), nil
}

// useRandSource points crypto/rand.Reader at src until the test ends;
// crypto/rand.Read draws from Reader once it has been replaced. Tests that
// call it must not run in parallel.
func useRandSource(t *testing.T, src io.Reader) {
	t.Helper()
	saved := rand.Reader
	rand.Reader = src
	t.Cleanup(func() { rand.Reader = saved })
}

// tokenMinters are the two generators, each run on a fresh copy of every
// source in tokenDraws.
var tokenMinters = []struct {
	name string
	mint func() (string, error)
}{
	{"interviews.generateToken", generateToken},
	{"users.NewToken", users.NewToken},
}

// tokenDraws is the token table: a random source and the tokens successive
// draws from it mint. A nil source is the real crypto/rand.Reader, whose
// draws are held to the format alone and must not repeat.
var tokenDraws = []struct {
	name string
	src  func() io.Reader
	want []string
}{
	{"counting bytes: 32 bytes a token, in order, as hex", func() io.Reader { return &countingSource{} }, []string{
		"000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f",
		"202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f",
	}},
	{"all 0xff: lowercase a-f", func() io.Reader { return constSource(0xff) }, []string{
		"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
	}},
	{"all zero: leading zeros kept", func() io.Reader { return constSource(0) }, []string{
		strings.Repeat("0", 64),
	}},
	{"crypto/rand", nil, nil},
}

// tokenHashes is the hash table: SHA-256 of the input's bytes as lowercase
// hex, computed independently of Go (Python's hashlib). The leading space,
// the trailing newline and the capitals show that nothing is normalised.
var tokenHashes = []struct{ in, want string }{
	{"", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
	{"abc", "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"},
	{" abc", "d92b1cb3a32147b86a4db0647e4bf6eda6cf160fd3b2da264c5b088c9f9ccbfa"},
	{"abc\n", "edeaaff3f1774ad2888673770c6d64097e391bc362d7d6fb34982ddf0efd18cb"},
	{"ABC", "b5d4045c3f466fa91fe2cc6abe79232a1a57cdf104f7a26e716e0a1e2789df78"},
	{"é", "4a99557e4033c3539de2eb65472017cad5f9557f7a0625a09f1c3f6e2ba69c4c"},
	{"The quick brown fox jumps over the lazy dog", "d7a8fbb307d7809469ca9abcb0082e4f8d5651e46d3cdb762d02d0bf37c9e592"},
	{"000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f", "6c86c6aac5fb24bcf5d9939cb7d7d5645ce39418f449e03b262dd4fa14b4b92b"},
	{"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", "df0790f236013511e91fa4532fb7761f62320a51a3868dabf4a13fe5f53e3263"},
}

// draw mints n tokens with mint, from src when it is not nil.
func draw(t *testing.T, mint func() (string, error), src func() io.Reader, n int) []string {
	t.Helper()
	if src != nil {
		useRandSource(t, src())
	}
	out := make([]string, n)
	for i := range out {
		tok, err := mint()
		if err != nil {
			t.Fatalf("draw %d: error %v", i, err)
		}
		out[i] = tok
	}
	return out
}

func TestGenerateTokenFormat(t *testing.T) {
	for _, c := range tokenDraws {
		for _, m := range tokenMinters {
			t.Run(c.name+"/"+m.name, func(t *testing.T) {
				n := len(c.want)
				if c.src == nil {
					n = 64
				}
				seen := map[string]bool{}
				for i, got := range draw(t, m.mint, c.src, n) {
					if !tokenFormat.MatchString(got) {
						t.Errorf("draw %d: %s() = %q (%d characters), want 64 lowercase hex digits", i, m.name, got, len(got))
					}
					if c.src != nil && got != c.want[i] {
						t.Errorf("draw %d: %s() = %q, want %q", i, m.name, got, c.want[i])
					}
					if c.src == nil && seen[got] {
						t.Errorf("draw %d: %s() repeated %q", i, m.name, got)
					}
					seen[got] = true
				}
			})
		}
	}
}

// TestGenerateTokenMatchesUsers pins that the copy mints what users.NewToken
// mints from the same random bytes.
func TestGenerateTokenMatchesUsers(t *testing.T) {
	for _, c := range tokenDraws {
		if c.src == nil {
			continue
		}
		t.Run(c.name, func(t *testing.T) {
			ours := draw(t, generateToken, c.src, len(c.want))
			theirs := draw(t, users.NewToken, c.src, len(c.want))
			for i := range ours {
				if ours[i] != theirs[i] {
					t.Errorf("draw %d: generateToken() = %q, users.NewToken() = %q; want them identical", i, ours[i], theirs[i])
				}
			}
		})
	}
}

func TestHashTokenVectors(t *testing.T) {
	for _, v := range tokenHashes {
		got := hashToken(v.in)
		if got != v.want {
			t.Errorf("hashToken(%q) = %q, want %q", v.in, got, v.want)
		}
		if !tokenFormat.MatchString(got) {
			t.Errorf("hashToken(%q) = %q, want 64 lowercase hex digits", v.in, got)
		}
	}
}

// TestHashTokenMatchesUsers pins that the copy hashes as users.HashToken
// does: every input of the table and 64 freshly minted tokens.
func TestHashTokenMatchesUsers(t *testing.T) {
	inputs := draw(t, users.NewToken, nil, 64)
	for _, v := range tokenHashes {
		inputs = append(inputs, v.in)
	}
	for _, in := range inputs {
		if ours, theirs := hashToken(in), users.HashToken(in); ours != theirs {
			t.Errorf("hashToken(%q) = %q, users.HashToken = %q; want them identical", in, ours, theirs)
		}
	}
}
