package main

import (
	"fmt"
	"go/ast"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
)

const mcpDir = "internal/mcp"

// mcpTool is what an mcp-tool scaffold names.
type mcpTool struct {
	Tool string // the tool's name, snake case
	Text string // the area in words, for a new file
	Ctor string // the constructor of a new area file
}

// mcpToolEntry is one Tool in the shape M11a gave the table: name,
// description, schema, then the handler and its one request.
var mcpToolEntry = template.Must(template.New("tool").Parse(mcpToolText))

const mcpToolText = `		{
			Name:        "{{.Tool}}",
			Description: "TODO: say what {{.Tool}} does, and when an agent should call it.",
			InputSchema: schema([]string{"project_id"}, map[string]interface{}{
				"project_id": str("Project ID"),
			}),
			// TODO: the request this tool makes, its path exactly as the server
			// registers it (internal/api/testdata/routes.txt). Mark the tool
			// ReadOnly: true only if it sends nothing but GETs; unmarked, it is
			// a writer, granted to no read-only agent.
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				out, _, err := c.request("GET", "/api/v1/projects/"+strArg(args, "project_id"), nil, nil)
				return out, err
			},
		},
`

// mcpAreaFile is a new tools_<area>.go, as M11a wrote each.
var mcpAreaFile = template.Must(template.New("area").Parse(`// The MCP tools over {{.Text}}. Tools, in tools.go, concatenates each
// area's constructor in the table's order.

package mcp

// {{.Ctor}} returns the {{.Text}} tools, in Tools() order.
func {{.Ctor}}() []Tool {
	return []Tool{
` + mcpToolText + `	}
}
`))

// mcpTable is internal/mcp as the scaffold reads it.
type mcpTable struct {
	toolsGo *goFile
	concat  *ast.CallExpr      // Tools()'s slices.Concat(...)
	order   []string           // the constructors, in Tools() order
	ctors   map[string]*goFile // each constructor's file
	decls   map[string]*ast.FuncDecl
	tools   map[string]string // each tool's name, to its file
	names   map[string]string // the package's declarations
}

func readMCPTable(root string) (*mcpTable, error) {
	files, err := parseDir(root, mcpDir)
	if err != nil {
		return nil, err
	}
	t := &mcpTable{ctors: map[string]*goFile{}, decls: map[string]*ast.FuncDecl{}, tools: map[string]string{}, names: declared(files)}
	for _, f := range files {
		if strings.HasSuffix(f.path, "_test.go") {
			continue
		}
		if filepath.Base(f.path) == "tools.go" {
			t.toolsGo = f
		}
		for _, d := range f.ast.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil {
				t.ctors[fd.Name.Name], t.decls[fd.Name.Name] = f, fd
			}
		}
		ast.Inspect(f.ast, func(n ast.Node) bool {
			if lit := toolLiteral(n); lit != nil {
				for _, el := range lit.Elts {
					if name := toolName(el); name != "" {
						t.tools[name] = filepath.Base(f.path)
					}
				}
			}
			return true
		})
	}
	if t.toolsGo == nil {
		return nil, fmt.Errorf("%s: no tools.go", mcpDir)
	}
	if fd := findFunc(t.toolsGo, "Tools", false); fd != nil && len(fd.Body.List) == 1 {
		if ret, ok := fd.Body.List[0].(*ast.ReturnStmt); ok && len(ret.Results) == 1 {
			t.concat, _ = ret.Results[0].(*ast.CallExpr)
		}
	}
	if t.concat == nil {
		return nil, fmt.Errorf("%s: Tools() is not one return of slices.Concat(<constructor>(), ...) to append to", t.toolsGo.path)
	}
	for _, arg := range t.concat.Args {
		call, _ := arg.(*ast.CallExpr)
		if call == nil || len(call.Args) != 0 {
			return nil, fmt.Errorf("%s: Tools() concatenates something other than a constructor call", t.toolsGo.path)
		}
		id, _ := call.Fun.(*ast.Ident)
		if id == nil || t.ctors[id.Name] == nil {
			return nil, fmt.Errorf("%s: Tools() concatenates something other than a constructor call", t.toolsGo.path)
		}
		t.order = append(t.order, id.Name)
	}
	if len(t.order) == 0 {
		return nil, fmt.Errorf("%s: Tools() concatenates no constructor", t.toolsGo.path)
	}
	return t, nil
}

// toolLiteral returns n when it is a []Tool literal.
func toolLiteral(n ast.Node) *ast.CompositeLit {
	lit, ok := n.(*ast.CompositeLit)
	if !ok {
		return nil
	}
	if at, ok := lit.Type.(*ast.ArrayType); ok && at.Len == nil {
		if id, ok := at.Elt.(*ast.Ident); ok && id.Name == "Tool" {
			return lit
		}
	}
	return nil
}

// toolName is the literal Name of one element of a []Tool literal.
func toolName(el ast.Expr) string {
	lit, ok := el.(*ast.CompositeLit)
	if !ok {
		return ""
	}
	for _, e := range lit.Elts {
		if kv, ok := e.(*ast.KeyValueExpr); ok {
			if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "Name" {
				if val, ok := kv.Value.(*ast.BasicLit); ok {
					s, _ := strconv.Unquote(val.Value)
					return s
				}
			}
		}
	}
	return ""
}

func planMCPTool(root string, n name, area *name) (*plan, error) {
	t, err := readMCPTable(root)
	if err != nil {
		return nil, err
	}
	tool := mcpTool{Tool: n.snake()}
	if f, ok := t.tools[tool.Tool]; ok {
		return nil, fmt.Errorf("a tool named %q is already declared in %s/%s; pick another name", tool.Tool, mcpDir, f)
	}
	file := t.ctors[t.order[len(t.order)-1]].path
	if area != nil {
		file = mcpDir + "/tools_" + area.snake() + ".go"
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(file))); area != nil && os.IsNotExist(err) {
		return planMCPArea(t, file, tool, *area)
	}

	// The constructor this file declares that comes last in Tools().
	ctor := ""
	for _, c := range t.order {
		if t.ctors[c] != nil && t.ctors[c].path == file {
			ctor = c
		}
	}
	if ctor == "" {
		return nil, fmt.Errorf("%s declares no constructor that Tools() concatenates", file)
	}
	var lit *ast.CompositeLit
	for _, s := range t.decls[ctor].Body.List {
		if ret, ok := s.(*ast.ReturnStmt); ok && len(ret.Results) == 1 {
			lit = toolLiteral(ret.Results[0])
		}
	}
	if lit == nil || len(lit.Elts) == 0 {
		return nil, fmt.Errorf("%s: %s does not return a []Tool literal to append to", file, ctor)
	}
	var b strings.Builder
	if err := mcpToolEntry.Execute(&b, tool); err != nil {
		return nil, err
	}
	f := t.ctors[ctor]
	edited, err := f.insertBefore(lit.Rbrace, b.String())
	if err != nil {
		return nil, err
	}
	p := &plan{}
	p.edit(f, edited)
	where := fmt.Sprintf("%s is the last tool of %s (%s), after %s", tool.Tool, ctor, filepath.Base(file), toolName(lit.Elts[len(lit.Elts)-1]))
	if ctor == t.order[len(t.order)-1] {
		where += ", so it is the last of Tools()"
	}
	if area == nil {
		where += "; -area <area> appends to internal/mcp/tools_<area>.go instead"
	}
	p.notes = append(p.notes, where+". The table's order is behaviour (I11): tools/list serves it, and ReadOnlyToolNames() keeps "+
		"it in what the seeded interviewer is granted, so a tool is appended to its constructor, never inserted.")
	p.steps = mcpSteps(p.steps, tool.Tool)
	return p, nil
}

// planMCPArea writes a new area file with the tool and appends its
// constructor to Tools().
func planMCPArea(t *mcpTable, file string, tool mcpTool, area name) (*plan, error) {
	tool.Text, tool.Ctor = area.text(), area.lowerCamel()+"Tools"
	if err := refuseDeclared(t.names, mcpDir, tool.Ctor); err != nil {
		return nil, err
	}
	src, err := render(file, mcpAreaFile, tool)
	if err != nil {
		return nil, err
	}
	edited, err := t.toolsGo.insertBefore(t.concat.Rparen, "\t\t"+tool.Ctor+"(),\n")
	if err != nil {
		return nil, err
	}
	p := &plan{}
	p.create(file, src)
	p.edit(t.toolsGo, edited)
	p.notes = append(p.notes, fmt.Sprintf("%s() is the last constructor of Tools(), so %s is the table's last tool. The table's "+
		"order is behaviour (I11): tools/list serves it, and ReadOnlyToolNames() keeps it in what the seeded interviewer is "+
		"granted, so a constructor is appended, never inserted.", tool.Ctor, tool.Tool))
	p.steps = mcpSteps(p.steps, tool.Tool)
	return p, nil
}

func mcpSteps(steps []string, tool string) []string {
	return append(steps,
		"Write the tool's description and request (internal/runner/README.md, \"Add an MCP tool\"); every request must be a route of "+
			"internal/api/testdata/routes.txt.",
		"Give "+tool+" recording arguments that set every schema property in toolGoldenArgs (internal/mcp/tools_golden_test.go).",
		"Regenerate S7's golden: "+regenMCPTools,
		releaseNote("a new tool"))
}
