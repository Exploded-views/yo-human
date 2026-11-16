package repolint

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"sort"
	"strings"
)

// passkeyHomes are the only places the passkey principal kind may be named: the bus, which
// defines it, and the approval package, which is the only one that creates such a principal.
var passkeyHomes = []string{"internal/hosted/bus/", "internal/hosted/approval/"}

// PasskeyKind returns the Go files under root, outside the bus and the approval package, that
// name the passkey principal kind: the identifier PrincipalPasskey or KindPasskey, or the string
// "passkey" converted to PrincipalKind. Only the approval page, after a verified assertion, makes
// a passkey principal (constitution V), so no other code may mint or compare one by name. The
// findings are sorted by path.
func PasskeyKind(root string) ([]TagFinding, error) {
	fsys := os.DirFS(root)
	var found []TagFinding
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return skipDir(fsys, p, d)
		}
		if !strings.HasSuffix(p, ".go") || inPasskeyHome(p) {
			return nil
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		file, err := parser.ParseFile(token.NewFileSet(), p, data, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		if msg := passkeyUse(file); msg != "" {
			found = append(found, TagFinding{p, msg})
		}
		return nil
	})
	sort.SliceStable(found, func(i, j int) bool { return found[i].Path < found[j].Path })
	return found, err
}

func inPasskeyHome(p string) bool {
	for _, h := range passkeyHomes {
		if strings.HasPrefix(p, h) {
			return true
		}
	}
	return false
}

// passkeyUse is the first use of the passkey kind in file, or "".
func passkeyUse(file *ast.File) string {
	msg := ""
	ast.Inspect(file, func(n ast.Node) bool {
		if msg != "" {
			return false
		}
		switch n := n.(type) {
		case *ast.Ident:
			if n.Name == "PrincipalPasskey" || n.Name == "KindPasskey" {
				msg = "names " + n.Name + ": only internal/hosted/approval and internal/hosted/bus may name the passkey principal kind"
			}
		case *ast.CallExpr:
			if isPasskeyConversion(n) {
				msg = `converts "passkey" to PrincipalKind: only internal/hosted/approval and internal/hosted/bus may name the passkey principal kind`
			}
		}
		return true
	})
	return msg
}

// isPasskeyConversion matches PrincipalKind("passkey") and pkg.PrincipalKind("passkey").
func isPasskeyConversion(c *ast.CallExpr) bool {
	if len(c.Args) != 1 {
		return false
	}
	lit, ok := c.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING || lit.Value != `"passkey"` && lit.Value != "`passkey`" {
		return false
	}
	switch f := c.Fun.(type) {
	case *ast.Ident:
		return f.Name == "PrincipalKind"
	case *ast.SelectorExpr:
		return f.Sel.Name == "PrincipalKind"
	}
	return false
}
