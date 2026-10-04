package automations

import (
	"testing"
	"time"
)

func TestRenderPrompt(t *testing.T) {
	tests := []struct {
		name     string
		template string
		vars     map[string]string
		want     string
	}{
		{
			name:     "single placeholder",
			template: "Review artifact {{artifact_id}}",
			vars:     map[string]string{"artifact_id": "abc-123"},
			want:     "Review artifact abc-123",
		},
		{
			name:     "multiple placeholders",
			template: "{{event_type}} on {{entity_id}} in project {{project_id}}",
			vars: map[string]string{
				"event_type": "artifact.updated",
				"entity_id":  "e1",
				"project_id": "p1",
			},
			want: "artifact.updated on e1 in project p1",
		},
		{
			name:     "unknown placeholder renders empty",
			template: "hello {{missing}} world",
			vars:     map[string]string{},
			want:     "hello  world",
		},
		{
			name:     "nil vars",
			template: "hello {{missing}}",
			vars:     nil,
			want:     "hello ",
		},
		{
			name:     "whitespace inside braces",
			template: "id={{ key }}",
			vars:     map[string]string{"key": "v"},
			want:     "id=v",
		},
		{
			name:     "no placeholders",
			template: "plain text",
			vars:     map[string]string{"key": "v"},
			want:     "plain text",
		},
		{
			name:     "repeated placeholder",
			template: "{{a}}-{{a}}",
			vars:     map[string]string{"a": "x"},
			want:     "x-x",
		},
		{
			name:     "dotted and dashed keys",
			template: "{{work.item-id}}",
			vars:     map[string]string{"work.item-id": "wi-9"},
			want:     "wi-9",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RenderPrompt(tt.template, tt.vars)
			if got != tt.want {
				t.Errorf("RenderPrompt(%q) = %q, want %q", tt.template, got, tt.want)
			}
		})
	}
}

func TestNextAfter(t *testing.T) {
	base := time.Date(2026, 3, 1, 10, 2, 0, 0, time.UTC)

	tests := []struct {
		name    string
		expr    string
		want    time.Time
		wantErr bool
	}{
		{
			name: "every five minutes",
			expr: "*/5 * * * *",
			want: time.Date(2026, 3, 1, 10, 5, 0, 0, time.UTC),
		},
		{
			name: "daily at nine rolls to next day",
			expr: "0 9 * * *",
			want: time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC),
		},
		{
			name: "later same hour",
			expr: "30 10 * * *",
			want: time.Date(2026, 3, 1, 10, 30, 0, 0, time.UTC),
		},
		{
			name: "hourly on the hour",
			expr: "0 * * * *",
			want: time.Date(2026, 3, 1, 11, 0, 0, 0, time.UTC),
		},
		{
			name:    "invalid expression",
			expr:    "not a cron",
			wantErr: true,
		},
		{
			name:    "empty expression",
			expr:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NextAfter(tt.expr, base)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("NextAfter(%q) expected error, got %v", tt.expr, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("NextAfter(%q) unexpected error: %v", tt.expr, err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("NextAfter(%q, %v) = %v, want %v", tt.expr, base, got, tt.want)
			}
		})
	}
}

// scopeRepo stores automations in memory.
type scopeRepo struct {
	Repository
	byID map[string]*Automation
}

func (r *scopeRepo) Save(a *Automation) error   { c := *a; r.byID[a.ID] = &c; return nil }
func (r *scopeRepo) Update(a *Automation) error { c := *a; r.byID[a.ID] = &c; return nil }
func (r *scopeRepo) FindByID(id string) (*Automation, error) {
	if a, ok := r.byID[id]; ok {
		c := *a
		return &c, nil
	}
	return nil, nil
}

// TestAnAutomationsScope pins how a request names an automation's scope
// (#379 question 42): a project, or the whole workspace, which is stored as
// no project. Create takes an absent or empty project_id as the whole
// workspace (an empty one was stored as sent, which the project_id UUID
// column refused); update leaves the scope alone for an absent (or null)
// project_id, and moves the automation for any other: to the whole
// workspace for the empty string, as an empty agent_id or team_id clears
// the target.
func TestAnAutomationsScope(t *testing.T) {
	str := func(s string) *string { return &s }
	show := func(p *string) string {
		if p == nil {
			return "the whole workspace"
		}
		return *p
	}
	repo := &scopeRepo{byID: map[string]*Automation{}}
	svc := NewDefaultService(repo)
	for _, tc := range []struct {
		name    string
		project *string
		want    string
	}{
		{"no project_id", nil, "the whole workspace"},
		{"an empty project_id", str(""), "the whole workspace"},
		{"a project", str("proj-1"), "proj-1"},
	} {
		a, err := svc.Create(CreateAutomationRequest{OrgID: "org-1", Name: "Watch", AgentID: str("agent-1"),
			Kind: KindManual, ProjectID: tc.project})
		if err != nil {
			t.Fatalf("create with %s: %v", tc.name, err)
		}
		if got := show(repo.byID[a.ID].ProjectID); got != tc.want {
			t.Errorf("create with %s stored the scope %s, want %s", tc.name, got, tc.want)
		}
	}

	a, err := svc.Create(CreateAutomationRequest{OrgID: "org-1", Name: "Watch", AgentID: str("agent-1"),
		Kind: KindManual, ProjectID: str("proj-1")})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		project *string
		want    string
	}{
		{"no project_id", nil, "proj-1"},
		{"another project", str("proj-2"), "proj-2"},
		{"an empty project_id", str(""), "the whole workspace"},
		{"no project_id again", nil, "the whole workspace"},
		{"a project again", str("proj-1"), "proj-1"},
	} {
		updated, err := svc.Update(a.ID, UpdateAutomationRequest{ProjectID: tc.project})
		if err != nil {
			t.Fatalf("update with %s: %v", tc.name, err)
		}
		if got := show(repo.byID[a.ID].ProjectID); got != tc.want || show(updated.ProjectID) != tc.want {
			t.Errorf("update with %s stored the scope %s and answered %s, want %s", tc.name, got,
				show(updated.ProjectID), tc.want)
		}
	}
}
