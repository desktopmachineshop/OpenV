package main

import (
	"fmt"
	"text/template"
)

const apiDir = "internal/api"

// apiArea is what an api-area scaffold names.
type apiArea struct {
	Module    string // the module path, for the domain import
	Text      string // the name in words
	Registrar string // register<Name>Routes
	Handler   string // List<Name>, the stub handler
	Helper    string // list<Name>, the stub's stand-in for a service
	Path      string // the stub's route template
}

// apiAreaFile follows internal/api/README.md: a registrar per area file,
// the handler guarded by a require* helper from authz.go, its errors
// written by httperr.go and its answer by respondJSON.
var apiAreaFile = template.Must(template.New("api-area").Parse(`package api

import (
	"net/http"

	"github.com/gorilla/mux"

	"{{.Module}}/internal/domain/members"
)

// {{.Registrar}} wires the area's routes.
// Its call is the last in RegisterRoutes (routes.go): gorilla/mux serves
// the first registered route that matches, so the call's position decides
// which of two overlapping templates answers a path (I2).
func (h *Handler) {{.Registrar}}(router *mux.Router) {
	router.HandleFunc("{{.Path}}", h.{{.Handler}}).Methods("GET")
}

// {{.Handler}} answers a project's {{.Text}}.
// TODO: say what it answers, and to whom.
func (h *Handler) {{.Handler}}(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}
	items, err := h.{{.Helper}}(projectID)
	if err != nil {
		respondInternal(w, r, "failed to load {{.Text}}", err)
		return
	}
	respondJSON(w, http.StatusOK, items)
}

// {{.Helper}} stands in for the area's domain service.
// TODO: give the service a HandlerDeps field (handlers.go) and its line in
// cmd/server/wire_http.go, call it from {{.Handler}}, and delete this.
func (h *Handler) {{.Helper}}(projectID string) ([]string, error) {
	return []string{}, nil
}
`))

func planAPIArea(root string, n name) (*plan, error) {
	file := apiDir + "/" + n.snake() + "_handlers.go"
	if err := refuseExisting(root, file); err != nil {
		return nil, err
	}
	mod, err := modulePath(root)
	if err != nil {
		return nil, err
	}
	a := apiArea{Module: mod, Text: n.text(), Registrar: "register" + n.camel() + "Routes",
		Handler: "List" + n.camel(), Helper: "list" + n.camel(), Path: "/api/v1/projects/{id}/" + n.kebab()}
	files, err := parseDir(root, apiDir)
	if err != nil {
		return nil, err
	}
	if err := refuseDeclared(declared(files), apiDir, a.Registrar, a.Handler, a.Helper); err != nil {
		return nil, err
	}
	route := "GET " + a.Path
	if taken, err := hasLine(root, apiDir+"/testdata/routes.txt", route); err != nil {
		return nil, err
	} else if taken {
		return nil, fmt.Errorf("%s is already a route (%s/testdata/routes.txt); pick another name", route, apiDir)
	}

	routes, err := parseGo(root, apiDir+"/routes.go")
	if err != nil {
		return nil, err
	}
	fd := findFunc(routes, "RegisterRoutes", true)
	if fd == nil || len(fd.Recv.List[0].Names) != 1 || fd.Type.Params.NumFields() != 1 || len(fd.Type.Params.List[0].Names) != 1 {
		return nil, fmt.Errorf("%s: no method RegisterRoutes(router) with a named receiver to append to", routes.path)
	}
	call := fmt.Sprintf("%s.%s(%s)", fd.Recv.List[0].Names[0].Name, a.Registrar, fd.Type.Params.List[0].Names[0].Name)
	edited, err := routes.insertBefore(fd.Body.Rbrace, "\t"+call+"\n")
	if err != nil {
		return nil, err
	}
	src, err := render(file, apiAreaFile, a)
	if err != nil {
		return nil, err
	}

	p := &plan{}
	p.create(file, src)
	p.edit(routes, edited)
	p.notes = append(p.notes, fmt.Sprintf("%s is the last call in RegisterRoutes. Position is behaviour (I2): gorilla/mux serves the first "+
		"registered route that matches, so routes registered last can take no path an earlier template serves. Move the call up only "+
		"to make one of its templates win over an overlapping one, and say so in the pull request.", call))
	p.steps = append(p.steps,
		"Write the handler and the service it calls (internal/api/README.md, \"Add an API area\" and \"Add an endpoint to an area\"). "+
			"A new service is one HandlerDeps field, plus its Handler field and copy in NewHandler until M14, plus one line in "+
			"cmd/server/wire_http.go (K5).",
		"Regenerate the route goldens: "+regenRoutes,
		"The tour and its S5e matrix send every route of internal/api/testdata/routes.txt: give "+route+" its case and regenerate "+
			"their goldens with a database (cmd/server/README.md, \"Guards\", S5).",
		"The client call goes in its area's module (frontend/src/api/README.md), or in a new one whose stub calls this route: "+
			"node frontend/scripts/scaffold.mjs api-module "+n.kebab(),
		releaseNote("a new route"))
	return p, nil
}

// The regenerate commands, exactly as the area READMEs print them
// (TestRegenerateCommandsAreTheREADMEs).
const (
	regenRoutes    = "UPDATE_ROUTES=1 go test ./internal/api -count=1 -v -run 'TestRouteInventory|TestRouteBinding'"
	regenMigration = "OPENV_TEST_DATABASE_URL=<server URL> UPDATE_GOLDEN=1 go test ./internal/persistence/postgres -count=1 -run '^(TestMigrationFreeze|TestEveryBootFreeze|TestSchemaGolden|TestPurgeCatalog)$'"
	regenMCPTools  = "UPDATE_GOLDEN=1 go test ./internal/mcp ./internal/runner -run TestMCPToolsGolden"
)

func releaseNote(what string) string {
	return "Release note: " + what + " changes behaviour, so the pull request adds a bullet under \"## Unreleased\" in " +
		"RELEASE_NOTES.md, in its group (CONTRIBUTING.md); one under \"### New features\" also registers a feature key in " +
		"internal/domain/release/features.go and gates on it (docs/release-policy.md)."
}
