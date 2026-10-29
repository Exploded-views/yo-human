package repolint

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

// The paste forms a real map is likely to take (round 4, MED-1): a clone URL, scp style, the slug
// first, a key of the slug, key=value, and a slug far from the name. Built at run time like the
// rest, so this file holds none of them.
func TestCodenameMapLinesFindsEveryPasteForm(t *testing.T) {
	cn := "cn:" + "alpha"
	for name, line := range map[string]string{
		"clone URL":        cn + " https://github.com/acme/widgets",
		"scp style":        cn + " git@github.com:acme/widgets.git",
		"slug first":       "acme/widgets " + cn,
		"slug as the key":  `{"acme/widgets": "` + cn + `"}`,
		"key=value":        cn + " repo=acme/widgets",
		"far padding":      cn + strings.Repeat(" ", 30) + "acme/widgets",
		"URL first":        "https://github.com/acme/widgets " + cn,
		"URL without http": cn + " github.com/acme/widgets",
	} {
		root := t.TempDir()
		plant(t, root, "f.txt", "x\n"+line+"\n")
		got, err := CodenameMapLines(root, []string{"f.txt"})
		if want := []LineFinding{{"f.txt", 2}}; err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%s: findings = %v, %v; want %v", name, got, err, want)
		}
	}
}

// A line holding a codename and only a ref, or the policy's cn/<name> form, is not an entry; in a
// fixture example/<name> is allowed whichever side of the codename it is on.
func TestCodenameMapLinesIgnoresRefsAndExamples(t *testing.T) {
	cn := "cn:" + "alpha"
	root := t.TempDir()
	plant(t, root, "a.md", cn+" release/1.2\n"+cn+" refs/heads/main\n"+cn+" is cn/alpha in the policy\n"+
		`d.Repo, d.Ref = "`+cn+`-1", "release/1.2"`+"\n")
	plant(t, root, "x_test.go", "example/alpha "+cn+"\n")
	got, err := CodenameMapLines(root, []string{"a.md", "x_test.go"})
	if err != nil || len(got) != 0 {
		t.Errorf("findings = %v, %v; want none", got, err)
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
	// Only the root's dist and .cache are ignored: the same names deeper down are walked.
	for _, f := range []string{"d/dist/n.go", "d/.cache/m.go", "dist/sub/dist/o.go"} {
		plant(t, root, f, "x")
	}
	got, err = RepoFiles(root)
	if want := []string{"a.md", "d/.cache/m.go", "d/b.go", "d/dist/n.go", "d/testdata/c.txt"}; err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("RepoFiles with nested dist = %v, %v; want %v", got, err, want)
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
