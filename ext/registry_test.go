package ext

import (
	"context"
	"errors"
	"testing"
)

type namedModule string

func (m namedModule) Name() string { return string(m) }

type bootStub struct{}

func (bootStub) Context() context.Context     { return context.Background() }
func (bootStub) Workspaces() WorkspaceService { return nil }
func (bootStub) Users() UserService           { return nil }
func (bootStub) Metrics() any                 { return nil }

// withRegistry runs f against an empty registry and restores the real one.
func withRegistry(t *testing.T, f func()) {
	t.Helper()
	registryMu.Lock()
	saved := registry
	registry = nil
	registryMu.Unlock()
	defer func() {
		registryMu.Lock()
		registry = saved
		registryMu.Unlock()
	}()
	f()
}

func TestRegisteredModulesAreBuiltInOrder(t *testing.T) {
	withRegistry(t, func() {
		Register("one", func(Boot) (Module, error) { return namedModule("one"), nil })
		Register("two", func(Boot) (Module, error) { return namedModule("two"), nil })
		if !Registered("one") || Registered("three") {
			t.Fatal("Registered does not answer what was registered")
		}
		mods, err := Build(bootStub{})
		if err != nil || len(mods) != 2 || mods[0].Name() != "one" || mods[1].Name() != "two" {
			t.Fatalf("Build = %v, %v", mods, err)
		}
	})
}

func TestAModuleThatCannotBeBuiltStopsTheBuild(t *testing.T) {
	withRegistry(t, func() {
		boom := errors.New("bad setting")
		Register("bad", func(Boot) (Module, error) { return nil, boom })
		if _, err := Build(bootStub{}); !errors.Is(err, boom) {
			t.Fatalf("Build error = %v, want the factory's", err)
		}
	})
}

func TestANameRegisteredTwicePanics(t *testing.T) {
	withRegistry(t, func() {
		Register("dup", func(Boot) (Module, error) { return namedModule("dup"), nil })
		defer func() {
			if recover() == nil {
				t.Fatal("a second registration under one name did not panic")
			}
		}()
		Register("dup", func(Boot) (Module, error) { return namedModule("dup"), nil })
	})
}
