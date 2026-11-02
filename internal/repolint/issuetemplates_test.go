package repolint

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The repo has a bug and a friction issue form and a labels list, so its issues come in a known
// shape. Each bad tree breaks one rule of the good tree.
func TestIssueTemplatesFixtures(t *testing.T) {
	for name, want := range map[string][]string{
		"good":                 nil,
		"bad-missing-friction": {".github/ISSUE_TEMPLATE/friction.yml is missing"},
		"bad-missing-id":       {".github/ISSUE_TEMPLATE/friction.yml has no field cost"},
		"bad-unlisted-label":   {`label "triage" is used by a template but not in .github/labels.yml`},
		"bad-color":            {`.github/labels.yml: label "bug" color "#d73a4a" is not 6 hex digits`},
		"bad-field-type":       {".github/ISSUE_TEMPLATE/bug.yml field platform is textarea, want input"},
	} {
		got, err := IssueTemplates("testdata/issuetemplates/" + name)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s:\n got %q\nwant %q", name, got, want)
		}
	}
}

func TestIssueTemplatesFailsWithoutTheDirectory(t *testing.T) {
	if _, err := IssueTemplates("testdata/issuetemplates/missing"); err == nil {
		t.Error("a root with no ISSUE_TEMPLATE directory passed; want an error")
	}
}

// TestIssueTemplatesBrokenTrees breaks one file of a copy of the good tree per case. An edit with
// an empty old replaces the whole file with new; an edit with an empty old and new removes it.
func TestIssueTemplatesBrokenTrees(t *testing.T) {
	type edit struct{ file, old, with string }
	const (
		bug      = ".github/ISSUE_TEMPLATE/bug.yml"
		friction = ".github/ISSUE_TEMPLATE/friction.yml"
		labels   = ".github/labels.yml"
	)
	for name, c := range map[string]struct {
		edit edit
		want string
	}{
		"a form that does not parse":         {edit{bug, "", "name: [\n"}, bug + " does not parse: "},
		"a form with no name":                {edit{friction, "name: Friction", `name: ""`}, friction + " has no name"},
		"a form with no description":         {edit{bug, "description: Fixture bug form", `description: ""`}, bug + " has no description"},
		"a form without its own label":       {edit{bug, `labels: ["bug"]`, `labels: ["docs"]`}, bug + " does not carry its label bug"},
		"a required field that is optional":  {edit{bug, "id: ran\n    validations:\n      required: true", "id: ran\n    validations:\n      required: false"}, bug + " field ran is not required"},
		"no bug form":                        {edit{bug, "", ""}, bug + " is missing"},
		"no labels list":                     {edit{labels, "", ""}, labels + " is missing"},
		"a labels list that does not parse":  {edit{labels, "", "- name: [\n"}, labels + " does not parse: "},
		"a standard label not listed":        {edit{labels, "- name: dogfood\n  color: \"5319e7\"\n  description: Found while using yo-human on our own work\n", ""}, `.github/labels.yml has no standard label "dogfood"`},
		"a listed label with no description": {edit{labels, "description: Documentation", `description: ""`}, `.github/labels.yml: label "docs" has no description`},
	} {
		root := copyIssueTree(t)
		applyIssueEdit(t, root, c.edit.file, c.edit.old, c.edit.with)
		got, err := IssueTemplates(root)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if len(got) != 1 || !strings.HasPrefix(got[0], c.want) {
			t.Errorf("%s:\n got %q\nwant one problem starting %q", name, got, c.want)
		}
	}
}

// copyIssueTree copies the good fixture tree into a temp root.
func copyIssueTree(t *testing.T) string {
	t.Helper()
	const src = "testdata/issuetemplates/good"
	root := t.TempDir()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p) //nolint:gosec // a fixture in this package
		if err != nil {
			return err
		}
		dst := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// applyIssueEdit edits one file of a copied tree, as TestIssueTemplatesBrokenTrees describes.
func applyIssueEdit(t *testing.T, root, file, old, with string) {
	t.Helper()
	path := filepath.Join(root, file)
	switch {
	case old == "" && with == "":
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	case old == "":
		if err := os.WriteFile(path, []byte(with), 0o600); err != nil {
			t.Fatal(err)
		}
	default:
		data, err := os.ReadFile(path) //nolint:gosec // a copy this test made
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(string(data), old) != 1 {
			t.Fatalf("%s: %q occurs %d times, want once", file, old, strings.Count(string(data), old))
		}
		if err := os.WriteFile(path, []byte(strings.Replace(string(data), old, with, 1)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRepoIssueTemplates(t *testing.T) {
	got, err := IssueTemplates("../..")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > 0 {
		t.Errorf("the repo's issue forms and labels list disagree:\n%s", strings.Join(got, "\n"))
	}
}
