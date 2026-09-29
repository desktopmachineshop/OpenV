package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/providers"
)

// memProviderSettings is a provider-settings store kept in memory.
type memProviderSettings struct {
	rows map[string]*providers.ProviderSetting // by provider
}

func (m *memProviderSettings) Upsert(p *providers.ProviderSetting) error {
	m.rows[p.Provider] = p
	return nil
}

func (m *memProviderSettings) FindByProvider(orgID, provider string) (*providers.ProviderSetting, error) {
	return m.rows[provider], nil
}

func (m *memProviderSettings) List(orgID string) ([]*providers.ProviderSetting, error) {
	list := make([]*providers.ProviderSetting, 0, len(m.rows))
	for _, p := range m.rows {
		list = append(list, p)
	}
	return list, nil
}

// TestProviderDetectionRecordsEveryKnownProvider: a runner reports all of its
// adapters in one request, so a report can name a provider this server does
// not know beside ones it does (a runner newer than the server). Every known
// provider is recorded whatever order the report's map is ranged in, and the
// report is still answered 400 for the unknown one, as a report naming it
// alone is. The handler used to range over the map and stop at the first
// refusal, so the known provider was recorded or not by Go's map order:
// repeated here, a stop at the refusal shows within a few rounds.
func TestProviderDetectionRecordsEveryKnownProvider(t *testing.T) {
	const rounds = 200
	body := `{"tour-cli":{"version":"1"},"claude-code":{"version":"2.1","installed":true},` +
		`"gemini-cli":{"version":"0.9"},"a-cli":{"version":"3"}}`
	for i := 0; i < rounds; i++ {
		store := &memProviderSettings{rows: map[string]*providers.ProviderSetting{}}
		h := NewHandler(HandlerDeps{ProviderService: providers.NewDefaultService(store)})
		r := httptest.NewRequest(http.MethodPost, "/api/v1/provider-settings/detect", strings.NewReader(body))
		r = r.WithContext(context.WithValue(r.Context(), ctxWorkerOrg, "org-1"))
		w := httptest.NewRecorder()
		h.RecordProviderDetection(w, r)

		for _, known := range []string{"claude-code", "gemini-cli"} {
			if row := store.rows[known]; row == nil || row.LastDetected["version"] == nil {
				t.Fatalf("round %d: %s's detection was not recorded (answer %d %q)", i, known, w.Code, w.Body.String())
			}
		}
		if len(store.rows) != 2 {
			t.Fatalf("round %d: %d providers stored, want the 2 known ones", i, len(store.rows))
		}
		if w.Code != http.StatusBadRequest {
			t.Fatalf("round %d: status = %d, want 400 (body %q)", i, w.Code, w.Body.String())
		}
		// The refusal names the first unknown provider by name, so the
		// answer does not depend on the order either.
		if want := `unknown provider \"a-cli\"`; !strings.Contains(w.Body.String(), want) {
			t.Fatalf("round %d: body = %q, want it to name %s", i, w.Body.String(), want)
		}
	}
}
