package repolint

import (
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// TagFinding is a file that breaks the e2e build-tag rule.
type TagFinding struct {
	Path string
	Msg  string
}

// goFile is one parsed .go file: whether it builds only with the e2e tag, and its imports.
type goFile struct {
	path    string
	test    bool
	e2eOnly bool
	imports []string
}

var moduleLine = regexp.MustCompile(`(?m)^module\s+(\S+)\s*$`)

// E2EOnly checks the module at root. Every *_e2e.go file must build only with the e2e tag
// (`//go:build e2e`), and no non-test file that builds without the tag may import a package
// whose every non-test file needs it. It skips testdata/, .cache/ and dist/ directories and
// nested modules. Findings are sorted by path.
func E2EOnly(root string) ([]TagFinding, error) {
	fsys := os.DirFS(root)
	mod, err := fs.ReadFile(fsys, "go.mod")
	if err != nil {
		return nil, err
	}
	m := moduleLine.FindSubmatch(mod)
	if m == nil {
		return nil, fmt.Errorf("%s/go.mod: no module line", root)
	}
	files, err := goFiles(fsys)
	if err != nil {
		return nil, err
	}
	return tagFindings(files, e2eOnlyPackages(string(m[1]), files)), nil
}

// goFiles parses every .go file under fsys.
func goFiles(fsys fs.FS) ([]goFile, error) {
	var files []goFile
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return skipDir(fsys, p, d)
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		f, err := parseGoFile(fsys, p)
		files = append(files, f)
		return err
	})
	return files, err
}

func skipDir(fsys fs.FS, p string, d fs.DirEntry) error {
	if p == "." {
		return nil
	}
	if skipDirs[d.Name()] {
		return fs.SkipDir
	}
	if _, err := fs.Stat(fsys, path.Join(p, "go.mod")); err == nil {
		return fs.SkipDir
	}
	return nil
}

func parseGoFile(fsys fs.FS, p string) (goFile, error) {
	f := goFile{path: p, test: strings.HasSuffix(p, "_test.go")}
	data, err := fs.ReadFile(fsys, p)
	if err != nil {
		return f, err
	}
	file, err := parser.ParseFile(token.NewFileSet(), p, data, parser.ImportsOnly|parser.ParseComments)
	if err != nil {
		return f, err
	}
	expr, err := buildConstraint(file)
	if err != nil {
		return f, fmt.Errorf("%s: %w", p, err)
	}
	f.e2eOnly = expr != nil && needsE2E(expr)
	for _, imp := range file.Imports {
		ip, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return f, err
		}
		f.imports = append(f.imports, ip)
	}
	return f, nil
}

// buildConstraint returns the file's //go:build expression, or nil when it has none. As in
// go/build, only // line comments before the package clause count: a //go:build line inside a
// /* */ block comment is not a constraint. More than one //go:build line is an error.
func buildConstraint(file *ast.File) (constraint.Expr, error) {
	var expr constraint.Expr
	for _, g := range file.Comments {
		for _, c := range g.List {
			if c.Pos() >= file.Package || !constraint.IsGoBuild(c.Text) {
				continue
			}
			if expr != nil {
				return nil, fmt.Errorf("more than one //go:build line")
			}
			x, err := constraint.Parse(c.Text)
			if err != nil {
				return nil, err
			}
			expr = x
		}
	}
	return expr, nil
}

// needsE2E reports whether x is e2e itself, or an && chain holding a bare e2e with no || at
// any depth. A negated tag (!purego), e2e under ! and any || can build without e2e.
func needsE2E(x constraint.Expr) bool {
	switch x := x.(type) {
	case *constraint.TagExpr:
		return x.Tag == "e2e"
	case *constraint.AndExpr:
		return noOr(x) && (needsE2E(x.X) || needsE2E(x.Y))
	}
	return false
}

func noOr(x constraint.Expr) bool {
	switch x := x.(type) {
	case *constraint.OrExpr:
		return false
	case *constraint.AndExpr:
		return noOr(x.X) && noOr(x.Y)
	case *constraint.NotExpr:
		return noOr(x.X)
	}
	return true
}

// e2eOnlyPackages returns the import paths of the packages with at least one non-test file
// whose every non-test file needs the e2e tag.
func e2eOnlyPackages(module string, files []goFile) map[string]bool {
	only := map[string]bool{}
	for _, f := range files {
		if f.test {
			continue
		}
		ip := importPath(module, path.Dir(f.path))
		if all, seen := only[ip]; !seen || all {
			only[ip] = f.e2eOnly
		}
	}
	for ip, all := range only {
		if !all {
			delete(only, ip)
		}
	}
	return only
}

func importPath(module, dir string) string {
	if dir == "." {
		return module
	}
	return module + "/" + dir
}

func tagFindings(files []goFile, e2eOnly map[string]bool) []TagFinding {
	var found []TagFinding
	for _, f := range files {
		if strings.HasSuffix(f.path, "_e2e.go") && !f.e2eOnly {
			found = append(found, TagFinding{f.path, "an _e2e.go file must carry //go:build e2e"})
		}
		if f.test || f.e2eOnly {
			continue
		}
		for _, ip := range f.imports {
			if e2eOnly[ip] {
				found = append(found, TagFinding{f.path, "imports " + ip + ", which builds only with the e2e tag"})
			}
		}
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].Path < found[j].Path })
	return found
}
