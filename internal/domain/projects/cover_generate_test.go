package projects

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A JPEG's first bytes: enough for the content sniff to read it as one.
var jpegBytes = append([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00}, make([]byte, 64)...)

func openAIStub(t *testing.T, status int, answer string, seen *map[string]interface{}) *OpenAIImages {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/images/generations" || r.Header.Get("Authorization") != "Bearer sk-test" {
			t.Errorf("request %s %s with %q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		if seen != nil {
			_ = json.NewDecoder(r.Body).Decode(seen)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(answer))
	}))
	t.Cleanup(srv.Close)
	g := NewOpenAIImages("gpt-image-test")
	g.BaseURL = srv.URL
	return g
}

// The request names the configured model, one wide JPEG, and the prompt;
// the answer's base64 image comes back as bytes.
func TestOpenAIImagesGenerates(t *testing.T) {
	var seen map[string]interface{}
	g := openAIStub(t, 200, `{"data":[{"b64_json":"`+base64.StdEncoding.EncodeToString(jpegBytes)+`"}]}`, &seen)
	img, err := g.Generate(context.Background(), "sk-test", "A test rig")
	if err != nil || string(img) != string(jpegBytes) {
		t.Fatalf("Generate: %d bytes, %v", len(img), err)
	}
	for k, want := range map[string]interface{}{"model": "gpt-image-test", "prompt": "A test rig", "size": "1536x1024", "output_format": "jpeg", "n": 1.0} {
		if seen[k] != want {
			t.Errorf("request %s = %v, want %v", k, seen[k], want)
		}
	}
}

// The provider's refusal is ErrCoverProvider with its message, except a
// refused key, whose message (which quotes part of the key) is replaced.
func TestOpenAIImagesRefusals(t *testing.T) {
	_, err := openAIStub(t, 400, `{"error":{"message":"Your request was rejected by the safety system."}}`, nil).
		Generate(context.Background(), "sk-test", "x")
	if !errors.Is(err, ErrCoverProvider) || !strings.Contains(err.Error(), "safety system") {
		t.Errorf("400: %v; want ErrCoverProvider with the provider's message", err)
	}
	_, err = openAIStub(t, 401, `{"error":{"message":"Incorrect API key provided: sk-te**st."}}`, nil).
		Generate(context.Background(), "sk-test", "x")
	if !errors.Is(err, ErrCoverProvider) || strings.Contains(err.Error(), "sk-") || !strings.Contains(err.Error(), "API key was refused") {
		t.Errorf("401: %v; want the key refused, without the key", err)
	}
	_, err = openAIStub(t, 200, `{"data":[]}`, nil).Generate(context.Background(), "sk-test", "x")
	if !errors.Is(err, ErrCoverProvider) {
		t.Errorf("no image: %v; want ErrCoverProvider", err)
	}
}

type fakeGen struct {
	img   []byte
	err   error
	calls int
}

func (f *fakeGen) Generate(context.Context, string, string) ([]byte, error) {
	f.calls++
	return f.img, f.err
}

type storeRepo struct {
	CoverRepository
	mime string
	data []byte
}

func (r *storeRepo) SetCover(_, mime string, data []byte) (time.Time, error) {
	r.mime, r.data = mime, data
	return time.Now().UTC(), nil
}

// Generate stores the image as the type its bytes read as, and stores
// nothing for a bad prompt, a missing generator, or an answer that is no
// image.
func TestCoverServiceGenerate(t *testing.T) {
	repo := &storeRepo{}
	gen := &fakeGen{img: jpegBytes}
	svc := NewCoverService(repo).WithGenerator(gen)
	if _, err := svc.Generate(context.Background(), "p", "sk", "  A rig  "); err != nil || repo.mime != "image/jpeg" {
		t.Fatalf("Generate: %v, stored %q", err, repo.mime)
	}

	repo.mime = ""
	for _, prompt := range []string{"", "   ", strings.Repeat("é", MaxCoverPromptRunes+1)} {
		if _, err := svc.Generate(context.Background(), "p", "sk", prompt); !errors.Is(err, ErrCoverPrompt) {
			t.Errorf("prompt of %d runes: %v, want ErrCoverPrompt", len([]rune(prompt)), err)
		}
	}
	if _, err := svc.Generate(context.Background(), "p", "sk", strings.Repeat("é", MaxCoverPromptRunes)); err != nil {
		t.Errorf("a prompt of exactly %d runes: %v", MaxCoverPromptRunes, err)
	}
	repo.mime = ""
	gen.img = []byte("<html>not an image</html>")
	if _, err := svc.Generate(context.Background(), "p", "sk", "x"); !errors.Is(err, ErrCoverProvider) || repo.mime != "" {
		t.Errorf("an answer that is no image: %v, stored %q", err, repo.mime)
	}
	if _, err := NewCoverService(repo).Generate(context.Background(), "p", "sk", "x"); !errors.Is(err, ErrNoCoverGenerator) {
		t.Errorf("no generator: %v", err)
	}
}
