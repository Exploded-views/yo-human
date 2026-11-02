package repolint

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"gopkg.in/yaml.v3"
)

const (
	issueTemplateDir = ".github/ISSUE_TEMPLATE"
	issueLabelsFile  = ".github/labels.yml"
)

// issueField is one entry of a form body. A markdown block has no id, so no check reads it.
type issueField struct {
	Type        string `yaml:"type"`
	ID          string `yaml:"id"`
	Validations struct {
		Required bool `yaml:"required"`
	} `yaml:"validations"`
}

// issueForm is the part of a GitHub issue form that the checks read.
type issueForm struct {
	Name        string       `yaml:"name"`
	Description string       `yaml:"description"`
	Labels      []string     `yaml:"labels"`
	Body        []issueField `yaml:"body"`
}

// issueLabel is one entry of labels.yml.
type issueLabel struct {
	Name        string `yaml:"name"`
	Color       string `yaml:"color"`
	Description string `yaml:"description"`
}

// requiredField is a body field a form must have: its id, and the GitHub field type it must be.
type requiredField struct{ id, typ string }

// issueFormSpec is what one of the two forms must carry: its own label and its required fields.
type issueFormSpec struct {
	file, label string
	fields      []requiredField
}

var issueFormSpecs = []issueFormSpec{
	{"bug.yml", "bug", []requiredField{
		{"ran", "textarea"}, {"happened", "textarea"}, {"expected", "textarea"},
		{"version", "textarea"}, {"platform", "input"},
	}},
	{"friction.yml", "friction", []requiredField{
		{"slowed", "textarea"}, {"proposal", "textarea"}, {"cost", "textarea"},
	}},
}

// issueStandardLabels are the labels labels.yml must list, whatever the forms use.
var issueStandardLabels = []string{"bug", "friction", "docs", "dogfood", "area:notify"}

var hexColor = regexp.MustCompile(`^[0-9a-fA-F]{6}$`)

// IssueTemplates checks the issue forms under root/.github/ISSUE_TEMPLATE and the labels list in
// root/.github/labels.yml. The bug and friction forms must exist, parse, and carry a name, a
// description, their own label and their required fields, each required. Every label a form
// uses must be listed in labels.yml, which must list the standard labels, each with a 6-digit hex
// color and a description. It returns one problem per violation.
func IssueTemplates(root string) ([]string, error) {
	found, used, err := checkIssueForms(filepath.Join(root, issueTemplateDir))
	if err != nil {
		return nil, err
	}
	return append(found, checkIssueLabels(root, used)...), nil
}

// checkIssueForms checks every .yml form in dir against the two specs, and returns the labels all
// of them use.
func checkIssueForms(dir string) ([]string, map[string]bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	var found []string
	used := map[string]bool{}
	seen := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yml" {
			continue
		}
		found = append(found, checkIssueFile(dir, e.Name(), used, seen)...)
	}
	return append(found, missingIssueForms(seen)...), used, nil
}

// checkIssueFile parses one form, adds its labels to used, and checks it if it is one of the two
// specs. seen records a spec file as found even when it does not parse, so that it is reported
// once, as unparsable, and not again as missing.
func checkIssueFile(dir, name string, used, seen map[string]bool) []string {
	rel := issueTemplateDir + "/" + name
	spec, isSpec := issueSpecFor(name)
	if isSpec {
		seen[name] = true
	}
	var form issueForm
	if err := readYAML(filepath.Join(dir, name), &form); err != nil {
		return []string{rel + " does not parse: " + err.Error()}
	}
	for _, l := range form.Labels {
		used[l] = true
	}
	if !isSpec {
		return nil
	}
	return checkIssueForm(rel, form, spec)
}

// issueSpecFor returns the spec for a form file name, if it is one of the two.
func issueSpecFor(name string) (issueFormSpec, bool) {
	for _, spec := range issueFormSpecs {
		if spec.file == name {
			return spec, true
		}
	}
	return issueFormSpec{}, false
}

// missingIssueForms reports each spec file that no form in the directory matched.
func missingIssueForms(seen map[string]bool) []string {
	var found []string
	for _, spec := range issueFormSpecs {
		if !seen[spec.file] {
			found = append(found, issueTemplateDir+"/"+spec.file+" is missing")
		}
	}
	return found
}

// checkIssueForm checks one of the two forms against its spec.
func checkIssueForm(rel string, form issueForm, spec issueFormSpec) []string {
	var found []string
	if form.Name == "" {
		found = append(found, rel+" has no name")
	}
	if form.Description == "" {
		found = append(found, rel+" has no description")
	}
	if !hasLabel(form.Labels, spec.label) {
		found = append(found, rel+" does not carry its label "+spec.label)
	}
	for _, f := range spec.fields {
		found = append(found, checkIssueField(rel, form.Body, f)...)
	}
	return found
}

// checkIssueField finds the body field with the required id and checks its type and that it is
// required.
func checkIssueField(rel string, body []issueField, want requiredField) []string {
	for _, item := range body {
		if item.ID != want.id {
			continue
		}
		var found []string
		if item.Type != want.typ {
			found = append(found, fmt.Sprintf("%s field %s is %s, want %s", rel, want.id, item.Type, want.typ))
		}
		if !item.Validations.Required {
			found = append(found, rel+" field "+want.id+" is not required")
		}
		return found
	}
	return []string{rel + " has no field " + want.id}
}

// checkIssueLabels checks labels.yml: each listed label has a 6-digit hex color and a description,
// the standard labels are listed, and every label a form uses is listed.
func checkIssueLabels(root string, used map[string]bool) []string {
	var labels []issueLabel
	err := readYAML(filepath.Join(root, issueLabelsFile), &labels)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return []string{issueLabelsFile + " is missing"}
	case err != nil:
		return []string{issueLabelsFile + " does not parse: " + err.Error()}
	}
	found, listed := labelEntryFindings(labels)
	for _, name := range issueStandardLabels {
		if !listed[name] {
			found = append(found, fmt.Sprintf("%s has no standard label %q", issueLabelsFile, name))
		}
	}
	return append(found, unlistedFindings(used, listed)...)
}

// labelEntryFindings checks each listed label's color and description, and returns the names listed.
func labelEntryFindings(labels []issueLabel) ([]string, map[string]bool) {
	var found []string
	listed := map[string]bool{}
	for _, l := range labels {
		listed[l.Name] = true
		if !hexColor.MatchString(l.Color) {
			found = append(found, fmt.Sprintf("%s: label %q color %q is not 6 hex digits", issueLabelsFile, l.Name, l.Color))
		}
		if l.Description == "" {
			found = append(found, fmt.Sprintf("%s: label %q has no description", issueLabelsFile, l.Name))
		}
	}
	return found, listed
}

// unlistedFindings reports each label a form uses that labels.yml does not list, sorted.
func unlistedFindings(used, listed map[string]bool) []string {
	var unlisted []string
	for name := range used {
		if !listed[name] {
			unlisted = append(unlisted, name)
		}
	}
	sort.Strings(unlisted)
	var found []string
	for _, name := range unlisted {
		found = append(found, fmt.Sprintf("label %q is used by a template but not in %s", name, issueLabelsFile))
	}
	return found
}

// readYAML reads a YAML file into v.
func readYAML(path string, v any) error {
	data, err := os.ReadFile(path) //nolint:gosec // the repo the caller names
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, v)
}

func hasLabel(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
