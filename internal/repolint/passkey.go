package repolint

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
)

// passkeyHomes are the only directories (exactly, no subpackages) where the passkey principal
// kind may be named: the bus, which defines it, and the approval package, which is the only one
// that creates such a principal.
var passkeyHomes = map[string]bool{"internal/hosted/bus": true, "internal/hosted/approval": true}

// passkeyAllowFile lists, under root, the paths and directory prefixes (ending in "/") outside the
// homes where the string "passkey" is not the principal kind (the policy's mode of that name, for
// one). Each line is reviewed; # starts a comment.
const passkeyAllowFile = "internal/repolint/passkey-allow.txt"

// PasskeyKind returns the Go files under root, outside the bus and the approval package, that
// name the passkey principal kind: the identifier PrincipalPasskey or KindPasskey, or any constant
// expression that evaluates to the string "passkey" (a literal, a parenthesised or concatenated
// one, a hex escape, a named constant), which covers a composite literal, an assignment, a var, a
// conversion and an alias alike. Only the approval page, after a verified assertion, makes a
// passkey principal (constitution V). A path on the allowlist is skipped. The findings are
// sorted by path. A principal decoded from JSON is a runtime path this check can't see: the web
// layer never decodes a Principal's Kind from input (Task 9).
func PasskeyKind(root string) ([]TagFinding, error) {
	fsys := os.DirFS(root)
	allow, err := readAllowlist(fsys)
	if err != nil {
		return nil, err
	}
	byDir := map[string][]parsedFile{}
	err = fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return firstErr(err, d, fsys, p)
		}
		if !strings.HasSuffix(p, ".go") || passkeyHomes[path.Dir(p)] || allowed(allow, p) {
			return nil
		}
		f, err := parseFile(fsys, p)
		byDir[path.Dir(p)] = append(byDir[path.Dir(p)], f)
		return err
	})
	var found []TagFinding
	for _, files := range byDir {
		consts := stringConsts(files)
		for _, f := range files {
			if msg := passkeyUse(f.file, consts); msg != "" {
				found = append(found, TagFinding{f.path, msg})
			}
		}
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].Path < found[j].Path })
	return found, err
}

// firstErr is the walk's error, or the directory skip.
func firstErr(err error, d fs.DirEntry, fsys fs.FS, p string) error {
	if err != nil {
		return err
	}
	return skipDir(fsys, p, d)
}

func parseFile(fsys fs.FS, p string) (parsedFile, error) {
	data, err := fs.ReadFile(fsys, p)
	if err != nil {
		return parsedFile{path: p}, err
	}
	file, err := parser.ParseFile(token.NewFileSet(), p, data, parser.SkipObjectResolution)
	return parsedFile{p, file}, err
}

type parsedFile struct {
	path string
	file *ast.File
}

func readAllowlist(fsys fs.FS) ([]string, error) {
	data, err := fs.ReadFile(fsys, passkeyAllowFile)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			out = append(out, line)
		}
	}
	return out, nil
}

func allowed(allow []string, p string) bool {
	for _, a := range allow {
		if p == a || strings.HasSuffix(a, "/") && strings.HasPrefix(p, a) {
			return true
		}
	}
	return false
}

// stringConsts are the package's const declarations (not vars: a var is no constant) by name, as
// expressions to evaluate.
func stringConsts(files []parsedFile) map[string]ast.Expr {
	out := map[string]ast.Expr{}
	for _, f := range files {
		ast.Inspect(f.file, func(n ast.Node) bool {
			if gd, ok := n.(*ast.GenDecl); ok && gd.Tok == token.CONST {
				for _, spec := range gd.Specs {
					vs := spec.(*ast.ValueSpec) // a const declaration holds only value specs
					if len(vs.Names) == len(vs.Values) {
						for i, name := range vs.Names {
							out[name.Name] = vs.Values[i]
						}
					}
				}
			}
			return true
		})
	}
	return out
}

// passkeyUse is the first use of the passkey kind in file, or "".
func passkeyUse(file *ast.File, consts map[string]ast.Expr) string {
	msg := ""
	ast.Inspect(file, func(n ast.Node) bool {
		if msg != "" {
			return false
		}
		if id, ok := n.(*ast.Ident); ok && (id.Name == "PrincipalPasskey" || id.Name == "KindPasskey") {
			msg = "names " + id.Name + ": only internal/hosted/approval and internal/hosted/bus may name the passkey principal kind"
		}
		if e, ok := n.(ast.Expr); ok && constString(e, consts, 0) == "passkey" {
			msg = `has a constant "passkey": only internal/hosted/approval and internal/hosted/bus may name the passkey principal kind (a string that is not the kind goes on the allowlist)`
		}
		return true
	})
	return msg
}

// constString evaluates the expression kinds a string constant is written in: a literal, in
// parentheses, a concatenation, and a name bound by a const in the package.
// It returns "" for anything else, so an expression it can't evaluate is never flagged.
func constString(e ast.Expr, consts map[string]ast.Expr, depth int) string {
	if depth > 8 {
		return ""
	}
	sub := func(x ast.Expr) string { return constString(x, consts, depth+1) }
	switch e := e.(type) {
	case *ast.BasicLit:
		s, _ := strconv.Unquote(e.Value) // a non-string literal fails to unquote: ""
		return s
	case *ast.ParenExpr:
		return sub(e.X)
	case *ast.BinaryExpr:
		if e.Op == token.ADD {
			return sub(e.X) + sub(e.Y)
		}
	case *ast.Ident:
		if v, ok := consts[e.Name]; ok {
			return sub(v)
		}
	}
	return ""
}
