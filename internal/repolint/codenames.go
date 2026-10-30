package repolint

import (
	"io/fs"
	"os"
	"regexp"
	"strings"
)

// LineFinding is one line of a file that breaks a line rule.
type LineFinding struct {
	Path string
	Line int
}

var (
	// A codename: cn:<name>. codenameTailRE is the codename with whatever slug-like characters
	// follow it, removed before a line is searched for slugs, so a codename with a slash is no owner/name.
	codenameRE = regexp.MustCompile(`cn:[a-z][a-z0-9-]{0,39}`)
	// A codename glued to a slug with no separator (cn:alpha-acme/widgets) is read as one token and
	// is not flagged, on purpose: no map line is written that way, and flagging it would flag every
	// codename with a slash.
	codenameTailRE = regexp.MustCompile(`cn:[A-Za-z0-9/._-]*`)
	// An owner/name slug. It must start the line or follow a character that is not part of a path
	// or host name, so the tail of a URL or of a longer path is not read as a slug.
	slugRE = regexp.MustCompile(`(?:^|[^A-Za-z0-9._/-])([A-Za-z0-9][A-Za-z0-9_-]*/[A-Za-z0-9][A-Za-z0-9._-]*)`)
	// A repository address on any host, in any case: <host>[:/]owner/name, as a URL or an scp-style
	// address (github.com, gitlab.com, an enterprise host). It names a repository whatever else is
	// on the line. A host has at least one dot, so a bare path is not one.
	hostRE = regexp.MustCompile(`(?i)[a-z0-9-]+(?:\.[a-z0-9-]+)+[:/][a-z0-9][a-z0-9_-]*/[a-z0-9][a-z0-9._-]*`)
	// A local clone path under the home directory: ~/dir/owner/name.
	homeRE        = regexp.MustCompile(`~/[A-Za-z0-9._-]+/[A-Za-z0-9._-]+/[A-Za-z0-9._-]+`)
	exampleSlugRE = regexp.MustCompile(`^example/[A-Za-z0-9][A-Za-z0-9._-]*$`)
	// Slugs that are not repositories: a branch, a ref, and the policy's own cn/<name> form.
	notARepoOwners = []string{"release/", "refs/", "cn/"}
)

// CodenameMapLines returns the lines of the named files (paths relative to root) shaped like an
// entry of the codename map: a codename (cn:<name>) and, anywhere on the same line and in either
// order, an owner/name slug, a repository address on any host (<host>[:/]owner/name) or a ~/ clone path. The slugs release/<n>, refs/<x> and cn/<x> are not
// repositories and are ignored. The map lives
// only on the walled node's own host; a real one must never be committed. The one allowed slug is
// example/<name>, and only in a test file (_test.go) or under testdata/. Findings are in the
// order of paths, then of lines.
func CodenameMapLines(root string, paths []string) ([]LineFinding, error) {
	var found []LineFinding
	for _, p := range paths {
		data, err := fs.ReadFile(os.DirFS(root), p)
		if err != nil {
			return nil, err
		}
		fixture := strings.HasSuffix(p, "_test.go") || strings.HasPrefix(p, "testdata/") || strings.Contains(p, "/testdata/")
		for i, line := range strings.Split(string(data), "\n") {
			if isMappingLine(line, fixture) {
				found = append(found, LineFinding{p, i + 1})
			}
		}
	}
	return found, nil
}

func isMappingLine(line string, fixture bool) bool {
	if !codenameRE.MatchString(line) {
		return false
	}
	if hostRE.MatchString(line) || homeRE.MatchString(line) {
		return true
	}
	for _, m := range slugRE.FindAllStringSubmatch(codenameTailRE.ReplaceAllString(line, " "), -1) {
		slug := m[1]
		if hasAnyPrefix(slug, notARepoOwners) || fixture && exampleSlugRE.MatchString(slug) {
			continue
		}
		return true
	}
	return false
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// RepoFiles returns every file under root, relative and slash-separated, except those under the
// root's .git, .cache and dist directories (the ignored ones; the same names deeper down are
// source). The files git tracks are among them, so a test
// over RepoFiles can only see more than the tracked set; internal packages run no other program,
// so it doesn't ask git.
func RepoFiles(root string) ([]string, error) {
	var files []string
	err := fs.WalkDir(os.DirFS(root), ".", func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir() && (path == ".git" || path == ".cache" || path == "dist"):
			return fs.SkipDir
		case !d.IsDir():
			files = append(files, path)
		}
		return nil
	})
	return files, err
}
