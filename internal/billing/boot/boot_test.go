package boot

import (
	"context"
	"testing"

	"github.com/openv/requirements-platform/ext"
	"github.com/openv/requirements-platform/internal/billing"
)

type bootStub struct{}

func (bootStub) Context() context.Context         { return context.Background() }
func (bootStub) Workspaces() ext.WorkspaceService { return nil }
func (bootStub) Users() ext.UserService           { return nil }
func (bootStub) Metrics() any                     { return nil }

// Without a secret key the factory builds billing off, starting nothing,
// as the built-in stage does.
func TestWithoutAKeyBillingIsBuiltOff(t *testing.T) {
	m, err := Factory(billing.Config{}, billing.Limits{})(bootStub{})
	if err != nil {
		t.Fatal(err)
	}
	if m.Name() != "billing" {
		t.Fatalf("name = %q", m.Name())
	}
	if caps := m.(ext.CapabilityModule).Capabilities(); len(caps) != 0 {
		t.Fatalf("billing off offers %v", caps)
	}
}
