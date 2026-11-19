package projects

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

// Generating a project's cover photo from a prompt, with the workspace's own
// OpenAI API key (the one its admins paste for API-key runs, under
// CoverKeyEnv). The server asks the provider for one wide image and stores
// it as the cover, as if it had been uploaded.

// DefaultCoverImageModel is the OpenAI image model a cover is generated with
// unless OPENV_COVER_IMAGE_MODEL names another.
const DefaultCoverImageModel = "gpt-image-2"

// CoverKeyEnv names the workspace API key cover generation uses.
const CoverKeyEnv = "OPENAI_API_KEY"

// MaxCoverPromptRunes caps a cover prompt.
const MaxCoverPromptRunes = 1000

// coverImageSize is 3:2, which every GPT image model accepts and which a
// cover's crop to a wide banner or a row's tile loses little of.
const coverImageSize = "1536x1024"

// Errors a generation can fail with before or at the provider.
var (
	// ErrCoverPrompt refuses an empty or over-long prompt; the API answers 400.
	ErrCoverPrompt = fmt.Errorf("a cover prompt must be 1 to %d characters", MaxCoverPromptRunes)
	// ErrNoCoverGenerator is a server built without a generator.
	ErrNoCoverGenerator = errors.New("cover generation is not available on this server")
	// ErrCoverProvider wraps the provider's own refusal or failure; the API
	// answers 502 with its message.
	ErrCoverProvider = errors.New("the image provider could not generate a cover")
)

// ImageGenerator makes one image from a prompt with the given API key, and
// answers its bytes.
type ImageGenerator interface {
	Generate(ctx context.Context, apiKey, prompt string) ([]byte, error)
}

// OpenAIImages is ImageGenerator over OpenAI's image generation endpoint.
type OpenAIImages struct {
	BaseURL string // no trailing slash, e.g. https://api.openai.com/v1
	Model   string
	HTTP    *http.Client
}

// NewOpenAIImages builds the generator for model, with a timeout that
// covers a slow generation.
func NewOpenAIImages(model string) *OpenAIImages {
	if model == "" {
		model = DefaultCoverImageModel
	}
	return &OpenAIImages{BaseURL: "https://api.openai.com/v1", Model: model, HTTP: &http.Client{Timeout: 150 * time.Second}}
}

// Generate implements ImageGenerator: one JPEG of coverImageSize.
func (g *OpenAIImages) Generate(ctx context.Context, apiKey, prompt string) ([]byte, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"model":              g.Model,
		"prompt":             prompt,
		"n":                  1,
		"size":               coverImageSize,
		"quality":            "medium",
		"output_format":      "jpeg",
		"output_compression": 85,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.BaseURL+"/images/generations", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCoverProvider, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 3*MaxCoverBytes))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCoverProvider, err)
	}
	var out struct {
		Data []struct {
			B64JSON string `json:"b64_json"`
		} `json:"data"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &out)
	if resp.StatusCode >= 300 {
		msg := fmt.Sprintf("status %d", resp.StatusCode)
		switch {
		case resp.StatusCode == http.StatusUnauthorized:
			// The provider's own message quotes part of the key, which the
			// editor asking need not see.
			msg = "the workspace's OpenAI API key was refused; a workspace admin can replace it under Workspace settings, AI providers"
		case out.Error != nil && out.Error.Message != "":
			msg = out.Error.Message
		}
		return nil, fmt.Errorf("%w: %s", ErrCoverProvider, msg)
	}
	if len(out.Data) == 0 || out.Data[0].B64JSON == "" {
		return nil, fmt.Errorf("%w: the answer held no image", ErrCoverProvider)
	}
	img, err := base64.StdEncoding.DecodeString(out.Data[0].B64JSON)
	if err != nil {
		return nil, fmt.Errorf("%w: the image could not be decoded", ErrCoverProvider)
	}
	return img, nil
}

// coverImageTypes are the types a cover is stored as.
var coverImageTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

// generate makes a cover with gen and answers it with its sniffed type.
func generateCover(ctx context.Context, gen ImageGenerator, apiKey, prompt string) ([]byte, string, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" || utf8.RuneCountInString(prompt) > MaxCoverPromptRunes {
		return nil, "", ErrCoverPrompt
	}
	if gen == nil {
		return nil, "", ErrNoCoverGenerator
	}
	img, err := gen.Generate(ctx, apiKey, prompt)
	if err != nil {
		return nil, "", err
	}
	mime := http.DetectContentType(img)
	if !coverImageTypes[mime] {
		return nil, "", fmt.Errorf("%w: the answer was not an image", ErrCoverProvider)
	}
	if len(img) > MaxCoverBytes {
		return nil, "", fmt.Errorf("%w: the image is larger than 5 MB", ErrCoverProvider)
	}
	return img, mime, nil
}
