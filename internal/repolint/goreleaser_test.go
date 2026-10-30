package repolint

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Nit 3 of the M1 review: goreleaser builds exactly RELEASE_TARGETS, with cgo off and no tags,
// so the gates that read RELEASE_TARGETS see the release.
func TestGoreleaserTargetsFixtures(t *testing.T) {
	for name, want := range map[string][]string{
		"good":        nil,
		"extraos":     {"builds[0] builds windows/amd64, which is not in RELEASE_TARGETS", "builds[0] builds windows/arm64, which is not in RELEASE_TARGETS"},
		"missingarch": {"builds[0] does not build darwin/arm64, which RELEASE_TARGETS lists", "builds[0] does not build linux/arm64, which RELEASE_TARGETS lists"},
		"targetextra": {"builds[0] does not build windows/amd64, which RELEASE_TARGETS lists"},
		"cgoon":       {"builds[0] env must set CGO_ENABLED=0 and nothing else for CGO_ENABLED (has CGO_ENABLED=1)"},
		"noenv":       {"builds[0] env must set CGO_ENABLED=0 and nothing else for CGO_ENABLED (has none)"},
		"tagsflag":    {"builds[0] flags hold -tags=e2e; the release builds with no tags"},
		"tagskey":     {"builds[0] sets tags; the release builds with no tags"},
		"goflags":     {"builds[0] env sets GOFLAGS=-tags=e2e; the release builds with no GOFLAGS"},
		"ignore":      {"builds[0] has an ignore list; RELEASE_TARGETS must be goos x goarch exactly"},
		"twobuilds":   {"builds[1] builds linux/riscv64, which is not in RELEASE_TARGETS", "builds[1] does not build darwin/amd64, which RELEASE_TARGETS lists", "builds[1] does not build darwin/arm64, which RELEASE_TARGETS lists", "builds[1] does not build linux/amd64, which RELEASE_TARGETS lists", "builds[1] does not build linux/arm64, which RELEASE_TARGETS lists"},
		"nobuilds":    {".goreleaser.yaml has no builds"},
	} {
		got, err := GoreleaserTargets("testdata/goreleaser/" + name)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s:\n got %q\nwant %q", name, got, want)
		}
	}
}

func TestGoreleaserTargetsFailsOnWhatItCannotRead(t *testing.T) {
	if _, err := GoreleaserTargets("testdata/goreleaser/missing"); err == nil {
		t.Error("a directory with neither file passed; want an error")
	}
	for name, files := range map[string]map[string]string{
		"no .goreleaser.yaml":     {"RELEASE_TARGETS": "linux/amd64\n"},
		"not YAML":                {"RELEASE_TARGETS": "linux/amd64\n", ".goreleaser.yaml": "builds: [\n"},
		"RELEASE_TARGETS is bare": {"RELEASE_TARGETS": "# none\n\n", ".goreleaser.yaml": "builds: []\n"},
	} {
		root := t.TempDir()
		for p, body := range files {
			if err := os.WriteFile(filepath.Join(root, p), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := GoreleaserTargets(root); err == nil {
			t.Errorf("%s: passed; want an error", name)
		}
	}
}

func TestRepoGoreleaserMatchesReleaseTargets(t *testing.T) {
	got, err := GoreleaserTargets("../..")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > 0 {
		t.Errorf(".goreleaser.yaml and RELEASE_TARGETS disagree:\n%s", strings.Join(got, "\n"))
	}
}
