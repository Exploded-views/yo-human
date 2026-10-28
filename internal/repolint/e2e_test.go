package repolint

import (
	"go/parser"
	"go/token"
	"reflect"
	"testing"
)

func TestE2EOnlyFixtures(t *testing.T) {
	// testdata/e2e: tagged/ok_e2e.go carries the tag; untagged/x_e2e.go imports net/http with
	// no tag; wrongtag/y_e2e.go builds without e2e; hooks builds only with e2e and prod imports
	// it; a test file, an e2e file and a mixed package's importer are all fine.
	got, err := E2EOnly("testdata/e2e")
	if err != nil {
		t.Fatalf("E2EOnly: %v", err)
	}
	// Fix round 1: a header passes only as e2e or an && chain with a bare e2e. Negated tags
	// (!purego, !linux && !windows), e2e under ! (linux && !e2e), || anywhere, and a
	// //go:build line inside a /* */ block comment (which go/build ignores) all fail;
	// andlinux (e2e && linux) passes.
	msg := "an _e2e.go file must carry //go:build e2e"
	want := []TagFinding{
		{"andneg/x_e2e.go", msg},
		{"andnotor/x_e2e.go", msg},
		{"andor/x_e2e.go", msg},
		{"blockcomment/x_e2e.go", msg},
		{"negos/x_e2e.go", msg},
		{"negpurego/x_e2e.go", msg},
		{"orchain/x_e2e.go", msg},
		{"prod/prod.go", "imports example.com/m/hooks, which builds only with the e2e tag"},
		{"untagged/x_e2e.go", "an _e2e.go file must carry //go:build e2e"},
		{"wrongtag/y_e2e.go", "an _e2e.go file must carry //go:build e2e"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("E2EOnly =\n%v\nwant\n%v", got, want)
	}
}

func TestRepoE2EOnly(t *testing.T) {
	got, err := E2EOnly("../..")
	if err != nil {
		t.Fatalf("E2EOnly: %v", err)
	}
	for _, f := range got {
		t.Errorf("%s: %s", f.Path, f.Msg)
	}
}

func TestBuildConstraintTwoLinesIsAnError(t *testing.T) {
	src := "//go:build e2e\n//go:build linux\n\npackage x\n"
	file, err := parser.ParseFile(token.NewFileSet(), "x.go", src, parser.PackageClauseOnly|parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := buildConstraint(file); err == nil {
		t.Fatal("want an error for two //go:build lines")
	}
}

// gofmt would move it, so this case is a string, not a fixture file.
func TestBuildConstraintIgnoresLinesAfterThePackageClause(t *testing.T) {
	src := "package x\n\n//go:build e2e\n\nimport _ \"net/http\"\n"
	file, err := parser.ParseFile(token.NewFileSet(), "x_e2e.go", src, parser.ImportsOnly|parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	if expr, err := buildConstraint(file); err != nil || expr != nil {
		t.Fatalf("got %v, %v; want no constraint", expr, err)
	}
}
