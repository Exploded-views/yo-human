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
	// A mapping entry: a codename (cn:<name>), a separator of punctuation or space (" ", "=", " -> ",
	// `": "`) and an owner/name slug, with the rest of the line captured. The slug must follow the
	// separator directly, so a line that merely mentions a codename and, later, an unrelated
	// path such as release/1.2 is not an entry. A separator holding a closing quote and a comma
	// is the next argument of a call or literal, not a map entry (isMappingLine).
	codenameMapRE = regexp.MustCompile(`cn:[a-z][a-z0-9-]{0,39}([^A-Za-z0-9\n]{1,12})(.*)`)
	slugRE        = regexp.MustCompile(`[A-Za-z0-9][A-Za-z0-9._-]*/[A-Za-z0-9][A-Za-z0-9._-]*`)
	exampleSlugRE = regexp.MustCompile(`^example/[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

// CodenameMapLines returns the lines of the named files (paths relative to root) shaped like an
// entry of the codename map: cn:<name>, then an owner/name slug on the same line. The map lives
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
	for _, m := range codenameMapRE.FindAllStringSubmatch(line, -1) {
		sep, rest := m[1], m[2]
		nextArg := strings.Contains(sep, `",`) || strings.Contains(sep, "',")
		if loc := slugRE.FindStringIndex(rest); loc == nil || loc[0] != 0 || nextArg {
			continue
		}
		for _, slug := range slugRE.FindAllString(rest, -1) {
			if !fixture || !exampleSlugRE.MatchString(slug) {
				return true
			}
		}
	}
	return false
}

// RepoFiles returns every file under root, relative and slash-separated, except those under
// .git, .cache and dist (the ignored directories). The files git tracks are among them, so a test
// over RepoFiles can only see more than the tracked set; internal packages run no other program,
// so it doesn't ask git.
func RepoFiles(root string) ([]string, error) {
	var files []string
	err := fs.WalkDir(os.DirFS(root), ".", func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir() && path != "." && (d.Name() == ".git" || d.Name() == ".cache" || d.Name() == "dist"):
			return fs.SkipDir
		case !d.IsDir():
			files = append(files, path)
		}
		return nil
	})
	return files, err
}
