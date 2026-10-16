package ext

import (
	"context"
	"sync"

	"github.com/openv/requirements-platform/internal/domain/users"
)

// UserService reads and writes accounts.
type UserService = users.Service

// Boot is what the server lends a module while it builds it, at stage
// billing of cmd/server, before the API handler exists.
type Boot interface {
	// Context ends when the server shuts down; a module's background work
	// stops with it.
	Context() context.Context
	Workspaces() WorkspaceService
	Users() UserService
	// Metrics is the server's metrics collector, for a module that reports
	// through the core's own metrics; it asserts the interface it needs.
	Metrics() any
}

// Factory builds a module. An error stops the server, as a malformed
// setting of the core's own does.
type Factory func(b Boot) (Module, error)

type registration struct {
	name    string
	factory Factory
}

var (
	registryMu sync.Mutex
	registry   []registration
)

// Register adds a module the server builds and plugs in at boot, under the
// name its Module reports. A program that builds the OpenV server with
// modules of its own registers them from a package it links into
// cmd/server (docs/extending.md). A second registration under one name
// panics, since the routes would collide.
func Register(name string, f Factory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	for _, r := range registry {
		if r.name == name {
			panic("ext: module " + name + " registered twice")
		}
	}
	registry = append(registry, registration{name: name, factory: f})
}

// Registered reports whether a module is registered under name. The server
// leaves out a module of its own that one registered under the same name
// replaces: the hosted service's billing, while it is still built into
// this repository too.
func Registered(name string) bool {
	registryMu.Lock()
	defer registryMu.Unlock()
	for _, r := range registry {
		if r.name == name {
			return true
		}
	}
	return false
}

// Build builds every registered module, in registration order.
func Build(b Boot) ([]Module, error) {
	registryMu.Lock()
	regs := append([]registration(nil), registry...)
	registryMu.Unlock()
	out := make([]Module, 0, len(regs))
	for _, r := range regs {
		m, err := r.factory(b)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}
