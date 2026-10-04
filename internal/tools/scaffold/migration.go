package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
	"regexp"
	"strconv"
	"text/template"
)

const postgresDir = "internal/persistence/postgres"

// migrationFileRe matches a numbered migration's file, as M10 left them
// and TestEachMigrationFileRegistersItsVersion checks.
var migrationFileRe = regexp.MustCompile(`^migration_(\d{4})_[a-z0-9_]+\.go$`)

// migration is what a migration scaffold names.
type migration struct {
	Version int
	Name    string // the registry's Name and the file's suffix
	Func    string // m<NNNN><Name>
}

// migrationFile is the shape of every migration_<NNNN>_<name>.go: the
// one function, its comment saying what changes and why.
var migrationFile = template.Must(template.New("migration").Parse(`package postgres

import "database/sql"

// {{printf "%04d" .Version}}: TODO: say what this migration changes and why. It runs once,
// inside the transaction that records it, so no CREATE INDEX
// CONCURRENTLY. Once it has shipped it is frozen (I16): change the schema
// again with a new migration, never by editing this one.
func {{.Func}}(tx *sql.Tx) error {
	// TODO: the DDL, for example:
	//	_, err := tx.Exec(` + "`ALTER TABLE ... ADD COLUMN IF NOT EXISTS ...`" + `)
	//	return err
	return nil
}
`))

func planMigration(root string, n name) (*plan, error) {
	reg, err := parseGo(root, postgresDir+"/migrations.go")
	if err != nil {
		return nil, err
	}
	lit := varLiteral(reg, "migrations")
	if lit == nil {
		return nil, fmt.Errorf("%s: no var migrations = []Migration{...} to append to", reg.path)
	}
	last, names, err := registryEntries(reg, lit)
	if err != nil {
		return nil, err
	}
	if v, ok := names[n.snake()]; ok {
		return nil, fmt.Errorf("migration %04d is already named %q; pick another name", v, n.snake())
	}
	files, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(postgresDir), "migration_*.go"))
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		if m := migrationFileRe.FindStringSubmatch(filepath.Base(f)); m != nil {
			v, _ := strconv.Atoi(m[1])
			last = max(last, v)
		}
	}
	m := migration{Version: last + 1, Name: n.snake(), Func: fmt.Sprintf("m%04d%s", last+1, n.camel())}
	if m.Version > 9999 {
		return nil, fmt.Errorf("version %d does not fit the four digits of a migration's file name", m.Version)
	}
	file := fmt.Sprintf("%s/migration_%04d_%s.go", postgresDir, m.Version, m.Name)
	if err := refuseExisting(root, file); err != nil {
		return nil, err
	}
	pkg, err := parseDir(root, postgresDir)
	if err != nil {
		return nil, err
	}
	if err := refuseDeclared(declared(pkg), postgresDir, m.Func); err != nil {
		return nil, err
	}
	entry := fmt.Sprintf("{Version: %d, Name: %q, Run: %s}", m.Version, m.Name, m.Func)
	edited, err := reg.insertBefore(lit.Rbrace, "\t"+entry+",\n")
	if err != nil {
		return nil, err
	}
	src, err := render(file, migrationFile, m)
	if err != nil {
		return nil, err
	}

	p := &plan{}
	p.create(file, src)
	p.edit(reg, edited)
	p.notes = append(p.notes, fmt.Sprintf("%s is the last entry of migrations. The registry's order is the order every database "+
		"applies them in (I16): a version another branch also takes is a one-line conflict, settled by giving this one the next "+
		"free version (rename the file and the function to match).", entry))
	p.steps = append(p.steps,
		"Write the DDL in "+m.Func+" (internal/persistence/postgres/README.md, \"Add a migration\"; \"Add a field to an entity\" "+
			"for the repository edit, which has no scaffold).",
		"Regenerate the S3 goldens on a server with the vector and pg_trgm extensions (pgvector/pgvector:pg15): "+regenMigration,
		releaseNote("a migration"))
	return p, nil
}

// varLiteral returns the composite literal that initialises the
// package-level var name in f.
func varLiteral(f *goFile, name string) *ast.CompositeLit {
	for _, d := range f.ast.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, s := range gd.Specs {
			vs := s.(*ast.ValueSpec)
			for i, id := range vs.Names {
				if id.Name == name && i < len(vs.Values) {
					lit, _ := vs.Values[i].(*ast.CompositeLit)
					return lit
				}
			}
		}
	}
	return nil
}

// registryEntries reads the registry: its highest version, and each name
// with its version.
func registryEntries(f *goFile, lit *ast.CompositeLit) (int, map[string]int, error) {
	last, names := 0, map[string]int{}
	for _, el := range lit.Elts {
		entry, ok := el.(*ast.CompositeLit)
		if !ok {
			return 0, nil, fmt.Errorf("%s: a registry entry that is not a Migration literal", f.fset.Position(el.Pos()))
		}
		version, nm := -1, ""
		for _, e := range entry.Elts {
			kv, ok := e.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, _ := kv.Key.(*ast.Ident)
			val, _ := kv.Value.(*ast.BasicLit)
			if key == nil || val == nil {
				continue
			}
			switch key.Name {
			case "Version":
				version, _ = strconv.Atoi(val.Value)
			case "Name":
				nm, _ = strconv.Unquote(val.Value)
			}
		}
		if version < 0 {
			return 0, nil, fmt.Errorf("%s: a registry entry with no literal Version", f.fset.Position(entry.Pos()))
		}
		last = max(last, version)
		names[nm] = version
	}
	return last, names, nil
}
