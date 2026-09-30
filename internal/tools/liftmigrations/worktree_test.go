package main

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The plan (S14, "each has a test that runs it on d11dee8") asks for a run
// on the commit it was written against. CI checks out one commit, so
// d11dee8 is not there; this test runs the generator on this checkout
// instead, whose migrations 0002-0047 are d11dee8's, byte for byte in
// code (the S3 freeze says so), and whose 0048-0051 came after. Once M10
// has landed the generator has nothing left to do here, and the fixture
// tests keep covering it.

// skipCopy are the top-level entries of the repository the copy leaves out:
// version control, the frontend and its dependencies, the browser tests and
// build output. Nothing the Go checks below read is in them.
var skipCopy = map[string]bool{".git": true, ".claude": true, "frontend": true, "node_modules": true, "e2e": true, "dist": true}

// copyModule copies the repository into a temporary directory.
func copyModule(t *testing.T, repo string) string {
	t.Helper()
	root := t.TempDir()
	err := filepath.WalkDir(repo, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(repo, path)
		if err != nil || rel == "." {
			return err
		}
		if top := strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]; skipCopy[top] {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		dst := filepath.Join(root, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return os.MkdirAll(dst, 0o755)
		case info.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(target, dst)
		case !info.Mode().IsRegular():
			return nil
		}
		return copyFile(path, dst, info.Mode().Perm())
	})
	if err != nil {
		t.Fatalf("copying the repository: %v", err)
	}
	return root
}

func copyFile(src, dst string, perm fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// s3SourceTests are the stored-data freeze's tests that read the source,
// and the registry's own check; s3DBTests those that need a database: the
// schema and purge goldens and the migration runner's tests.
const (
	s3SourceTests = "^(TestMigrationFreeze|TestEveryBootFreeze|TestRegistryIsOrderedWithoutDB)$"
	s3DBTests     = "^(TestSchemaGolden|TestPurgeCatalog|TestMigrate.*|TestRegistryValidation|TestNumberedMigrationRunsExactlyOnce|" +
		"TestFailedMigrationRollsBack|TestHotPathIndexesCreated|TestVectorReconcile.*|TestTrgmReconcile.*|TestPersonalOrg.*)$"
)

// TestLiftTheWorkingTree runs M10's recipe (runRecipe) on a copy of this
// repository. It checks what M10 must show:
//
//   - declhash: the move leaves every declaration of the package identical
//     (class A); after the lift only the registry differs, and each
//     migration is a new m00NN function (class B);
//   - the package builds and vets, and so does the module;
//   - go test ./internal/archtest passes with ratchets.json as it is, so M10
//     adds no entry (it may remove migrations.go's, which it must commit);
//   - the stored-data freeze passes against its goldens as they are: every
//     migration's hash, what it reaches, and the every-boot code; with
//     OPENV_TEST_DATABASE_URL set, so do the schema and purge goldens and the
//     migration runner's tests (CI's backend job sets it);
//   - M10's done-when: migrations.go is at most 150 lines, and its registry
//     one line per entry.
//
// Once M10 has landed the lift has nothing to lift, and the test skips,
// whether or not a later change has put a declaration beside the registry
// for -spec to move (TestRecipeAfterM10).
func TestLiftTheWorkingTree(t *testing.T) {
	if testing.Short() {
		t.Skip("copies, builds and tests the module; go test without -short runs it (CI's backend job does)")
	}
	repo, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, defaultPackage, "migrations.go")); err != nil {
		t.Fatalf("no %s/migrations.go in %s: %v", defaultPackage, repo, err)
	}
	declhash, declmove := buildTool(t, "declhash"), buildTool(t, "declmove")
	r := runRecipe(t, declhash, declmove, repo, defaultPackage, "internal/tools/declmove/specs/M10.json")
	if !r.lifted {
		t.Skip("M10 has landed: every migration runs a named function, so the generator has nothing to lift; " +
			"the fixture tests cover it")
	}
	root := r.root
	checkDoneWhen(t, r.pkg)

	command(t, root, "go", "vet", "./"+defaultPackage)
	command(t, root, "go", "build", ".", "./cmd/...", "./internal/...")
	mustPass(t, root, "archtest on the lifted tree fails: M10 may not add a ratchets.json entry, so the generator's output must fit "+
		"the budgets as it is (K14: files of at most 800 lines, functions of at most 100)", "./internal/archtest")
	mustPass(t, root, "the stored-data freeze (S3) fails on the lifted tree: the generator changed what a migration or the "+
		"every-boot code does, which M10 must not", "-run", s3SourceTests, "./"+defaultPackage)
	if os.Getenv("OPENV_TEST_DATABASE_URL") == "" {
		t.Log("OPENV_TEST_DATABASE_URL is unset: the schema and purge goldens and the migration tests did not run on the lifted tree (CI's backend job runs them)")
		return
	}
	out := mustPass(t, root, "the schema or purge golden (S3), or a migration test, fails on the lifted tree: the generator "+
		"changed what the migrations do, which M10 must not", "-v", "-run", s3DBTests, "./"+defaultPackage)
	for _, name := range []string{"TestSchemaGolden", "TestPurgeCatalog", "TestMigrateFreshDatabase"} {
		if !regexp.MustCompile(`(?m)^--- PASS: `+name+` `).MatchString(out) || regexp.MustCompile(`--- SKIP: `+name+`( |/)`).MatchString(out) {
			t.Errorf("%s did not pass on the lifted tree (it skipped, or did not run):\n%s", name, out)
		}
	}
}

// recipe is what runRecipe did: the copy it ran in and the package's
// directory there, whether -spec moved anything and whether the lift lifted
// anything.
type recipe struct {
	root, pkg     string
	moved, lifted bool
}

// runRecipe runs M10's recipe on a copy of the module at repo, on its
// package rel, with the spec written to specRel in the copy: liftmigrations
// -spec, declmove (without goimports, which CI would have to download; M10
// runs it), then the lift. declhash proves that the move leaves every
// declaration identical (class A) and, when the lift lifted migrations,
// that only the registry changed and the m00NN functions were added (class
// B). A lift with nothing to lift means M10 has landed: whatever -spec moved
// (a declaration a later change put beside the registry), declhash has
// proved; if it moved nothing, the package must be as it was.
func runRecipe(t *testing.T, declhash, declmove, repo, rel, specRel string) recipe {
	t.Helper()
	root := copyModule(t, repo)
	r := recipe{root: root, pkg: filepath.Join(root, filepath.FromSlash(rel))}
	spec := filepath.Join(root, filepath.FromSlash(specRel))
	manifest := func(dir, name string) string {
		out := filepath.Join(t.TempDir(), name)
		command(t, dir, declhash, "-o", out, rel)
		return out
	}
	base := manifest(repo, "base.txt")

	code, stdout, stderr := runTool(t, root, "-spec", spec, r.pkg)
	if code != 0 {
		t.Fatalf("liftmigrations -spec exited %d:\n%s%s", code, stdout, stderr)
	}
	r.moved = !strings.Contains(stdout, "nothing to move")
	if r.moved {
		t.Logf("liftmigrations -spec:\n%s", stdout)
		command(t, root, declmove, "-goimports=false", "-spec", spec)
		t.Log(command(t, root, declhash, "-compare", base, manifest(root, "moved.txt")))
	}
	code, stdout, stderr = runTool(t, root, r.pkg)
	if code != 0 {
		t.Fatalf("the lift exited %d:\n%s%s", code, stdout, stderr)
	}
	if strings.Contains(stdout, "nothing to lift") {
		if !r.moved && !equalTrees(readTree(t, r.pkg), readTree(t, filepath.Join(repo, filepath.FromSlash(rel)))) {
			t.Fatal("the generator found nothing to do, but the package changed")
		}
		return r
	}
	t.Log(lastLine(stdout))
	checkLiftedManifest(t, declhash, rel, base, manifest(root, "lifted.txt"))
	r.lifted = true
	return r
}

// TestRecipeAfterM10 is what TestLiftTheWorkingTree meets once M10 has
// landed and a later change has put a declaration beside the registry: -spec
// moves it out, declhash proves the move, and the lift has nothing to lift,
// which is the skip, not a failure. The fixture stands in for the
// repository, lifted by the recipe first.
func TestRecipeAfterM10(t *testing.T) {
	declhash, declmove := buildTool(t, "declhash"), buildTool(t, "declmove")
	fixture, _ := copyFixture(t)
	m10 := runRecipe(t, declhash, declmove, fixture, "store", "M10.json")
	if !m10.moved || !m10.lifted {
		t.Fatalf("the recipe on the fixture: moved %v, lifted %v; want both", m10.moved, m10.lifted)
	}
	registry := filepath.Join(m10.pkg, "migrations.go")
	b, err := os.ReadFile(registry)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, registry, string(b)+"\n// latestVersion is the registry's last version, added beside it after M10.\nconst latestVersion = 8\n")

	after := runRecipe(t, declhash, declmove, m10.root, "store", "M10.json")
	if !after.moved || after.lifted {
		t.Fatalf("the recipe after M10: moved %v, lifted %v; want the new const moved and nothing lifted", after.moved, after.lifted)
	}
	if runner := readTree(t, after.pkg)["migrate_runner.go"]; !strings.Contains(runner, "const latestVersion = 8") {
		t.Errorf("the const beside the registry did not move to migrate_runner.go:\n%s", runner)
	}
}

// mustPass runs go test -count=1 in the copy and fails with the hint and
// the output if it fails.
func mustPass(t *testing.T, root, hint string, args ...string) string {
	t.Helper()
	cmd := append([]string{"test", "-count=1"}, args...)
	out, err := runGo(root, cmd...)
	if err != nil {
		t.Fatalf("%s.\ngo %s: %v\n%s", hint, strings.Join(cmd, " "), err, out)
	}
	return out
}

// checkLiftedManifest compares the package's declhash manifest before the
// recipe with the one after a lift that lifted migrations: the registry
// changed, one m00NN function per migration was added, and nothing else
// differs.
func checkLiftedManifest(t *testing.T, declhash, rel, base, lifted string) {
	t.Helper()
	out, _ := runCmd("", declhash, "-compare", base, lifted)
	added := regexp.MustCompile(`^added\t` + regexp.QuoteMeta(rel) + `\tm\d{4,}[A-Z][A-Za-z0-9]*$`)
	var unexpected, adds []string
	changed := false
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		switch {
		case strings.HasPrefix(line, "declhash:"):
		case line == "changed\t"+rel+"\tmigrations":
			changed = true
		case added.MatchString(line):
			adds = append(adds, line)
		default:
			unexpected = append(unexpected, line)
		}
	}
	switch {
	case len(unexpected) > 0:
		sort.Strings(unexpected)
		t.Fatalf("declhash after the recipe differs beyond the registry and the new m00NN functions:\n  %s\n(full comparison:\n%s)",
			strings.Join(unexpected, "\n  "), out)
	case !changed:
		t.Fatalf("the lift says it lifted migrations, but declhash after the recipe shows the registry unchanged:\n%s", out)
	case len(adds) == 0:
		t.Fatalf("the lift says it lifted migrations, but declhash after the recipe shows no new m00NN function:\n%s", out)
	}
	t.Logf("declhash: the registry changed and %d migration functions were added; every other declaration is identical", len(adds))
}

// checkDoneWhen checks M10's done-when on the lifted package: migrations.go
// is at most 150 lines, and the registry is one line per entry.
func checkDoneWhen(t *testing.T, pkg string) {
	t.Helper()
	p, err := loadPackage(pkg)
	if err != nil {
		t.Fatal(err)
	}
	reg, lit, err := p.registry()
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(reg.file.src), "\n"); n > 150 {
		t.Errorf("%s is %d lines after the recipe; M10's done-when is at most 150", reg.file.name, n)
	}
	lines := p.fset.Position(lit.Rbrace).Line - p.fset.Position(lit.Lbrace).Line - 1
	if lines != len(lit.Elts) {
		t.Errorf("the registry spans %d lines for %d entries; M10 makes it one line per entry", lines, len(lit.Elts))
	}
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	return s[strings.LastIndexByte(s, '\n')+1:]
}
