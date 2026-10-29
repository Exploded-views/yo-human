package repolint

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// mapLine builds a line shaped like a mapping entry at run time, so this file holds no such line
// itself (the repo-wide scan below reads this file too).
func mapLine(sep, slug string) string { return "cn:" + "alpha" + sep + slug }

func plant(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The probe: a planted mapping line is found, in a temp dir, never committed.
func TestCodenameMapLinesFindsAPlantedMapping(t *testing.T) {
	root := t.TempDir()
	plant(t, root, "notes.md", "intro\n"+mapLine(" ", "acme/widgets")+"\n")
	plant(t, root, "conf/codenames", mapLine("=", "Acme-Co/widgets.v2")+"\n"+mapLine(" -> ", "acme/widgets.git")+"\n")
	plant(t, root, "x.json", `{"`+"cn:"+`alpha": "acme/widgets"}`+"\n")
	plant(t, root, "y.go", "var m = `"+mapLine(": ", "acme/widgets")+"`\n")
	got, err := CodenameMapLines(root, []string{"notes.md", "conf/codenames", "x.json", "y.go"})
	if err != nil {
		t.Fatal(err)
	}
	want := []LineFinding{{"notes.md", 2}, {"conf/codenames", 1}, {"conf/codenames", 2}, {"x.json", 1}, {"y.go", 1}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("findings = %v, want %v", got, want)
	}
}

func TestCodenameMapLinesIgnoresWhatIsNotAMapping(t *testing.T) {
	root := t.TempDir()
	plant(t, root, "a.md", "cn:"+"alpha alone\n"+"cn/alpha acme/widgets\n"+"a codename is cn:<name>, never owner/name\n"+
		"cn:"+"Alpha acme/widgets\n"+"acme/widgets alone\n"+"cn:"+"o/alpha has no owner before the slash\n"+
		`d.Repo, d.Ref = "cn:`+`alpha-1", "release/1.2"`+"\n"+"a test wants cn:"+"alpha, got cn/alpha\n")
	got, err := CodenameMapLines(root, []string{"a.md"})
	if err != nil || len(got) != 0 {
		t.Errorf("findings = %v, %v; want none", got, err)
	}
}

// example/<name> is the one allowed target, in test fixtures only.
func TestCodenameMapLinesAllowsExampleInFixtures(t *testing.T) {
	root := t.TempDir()
	ex := mapLine(" ", "example/alpha") + "\n"
	plant(t, root, "pkg/x_test.go", ex)
	plant(t, root, "pkg/testdata/map.txt", ex)
	plant(t, root, "README.md", ex)
	plant(t, root, "pkg/mixed_test.go", mapLine(" ", "example/alpha acme/widgets")+"\n")
	plant(t, root, "pkg/real_test.go", mapLine(" ", "acme/widgets")+"\n")
	got, err := CodenameMapLines(root, []string{"pkg/x_test.go", "pkg/testdata/map.txt", "README.md", "pkg/mixed_test.go", "pkg/real_test.go"})
	if err != nil {
		t.Fatal(err)
	}
	want := []LineFinding{{"README.md", 1}, {"pkg/mixed_test.go", 1}, {"pkg/real_test.go", 1}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("findings = %v, want %v", got, want)
	}
}

func TestCodenameMapLinesReportsAnUnreadableFile(t *testing.T) {
	if _, err := CodenameMapLines(t.TempDir(), []string{"missing.md"}); err == nil {
		t.Error("no error for a missing file")
	}
}

func TestRepoFilesSkipsIgnoredDirectories(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{"a.md", "d/b.go", "d/testdata/c.txt", ".git/x", ".cache/y", "dist/z"} {
		plant(t, root, f, "x")
	}
	got, err := RepoFiles(root)
	if want := []string{"a.md", "d/b.go", "d/testdata/c.txt"}; err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("RepoFiles = %v, %v; want %v", got, err, want)
	}
	if _, err := RepoFiles(filepath.Join(root, "missing")); err == nil {
		t.Error("no error for a missing root")
	}
}

// The wall's whole premise: a real codename mapping never enters this repository (the plan's
// "codename map" bullet). Only example/<name> in test fixtures may follow a cn: name.
func TestNoCodenameMapInRepo(t *testing.T) {
	files, err := RepoFiles("../..")
	if err != nil {
		t.Fatal(err)
	}
	found, err := CodenameMapLines("../..", files)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range found {
		t.Errorf("%s:%d: a codename mapping line (cn:<name> then owner/name); only example/<name> in a test fixture is allowed", f.Path, f.Line)
	}
}
