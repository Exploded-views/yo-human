package repolint

import (
	"go/ast"
	"go/token"
	"io/fs"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
)

// sessionFreeRoots are the trees that must never reach a session: hosted mode, and its three
// commands. The bus pushes to the owner and is read by sessions; it never wakes one (plan,
// Global Constraints, "Session transport").
var sessionFreeRoots = []string{"internal/hosted", "cmd/yo-human-hosted", "cmd/yo-human-desk", "cmd/yo-human-approved"}

var dialNames = map[string]bool{"Dial": true, "DialContext": true, "DialTimeout": true}

var unixNetworks = map[string]bool{"unix": true, "unixgram": true, "unixpacket": true}

// NeverWakesSessions returns the Go files, tests included, under the session-free roots of root
// that give hosted mode a way to reach a session: an import path or a string literal containing
// herdr (a comment does not count); a call to net.DialTimeout, or to any .Dial, .DialContext or
// .DialTimeout selector, with a unix, unixgram or unixpacket network literal; and any call to
// net.DialUnix or net.UnixAddr literal. It resolves the name net is imported under, so an
// aliased or dot import doesn't evade it. A network passed as a variable or constant can't be
// checked statically: that is best-effort, and review covers it. Listening on a socket isn't
// covered, since it reaches no session. The findings are sorted by path.
func NeverWakesSessions(root string) ([]TagFinding, error) {
	fsys := os.DirFS(root)
	var found []TagFinding
	for _, dir := range sessionFreeRoots {
		if _, err := fs.Stat(fsys, dir); err != nil {
			continue
		}
		err := fs.WalkDir(fsys, dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return firstErr(err, d, fsys, p)
			}
			if !strings.HasSuffix(p, ".go") {
				return nil
			}
			f, err := parseFile(fsys, p)
			if err != nil {
				return err
			}
			if msg := wakesSession(f.file); msg != "" {
				found = append(found, TagFinding{path.Clean(p), msg})
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].Path < found[j].Path })
	return found, nil
}

// wakesSession is the first way file reaches a session, or "".
func wakesSession(file *ast.File) string {
	netName, dot := netImportName(file)
	msg := ""
	ast.Inspect(file, func(n ast.Node) bool {
		if msg == "" {
			msg = nodeWakes(n, netName, dot)
		}
		return msg == ""
	})
	return msg
}

// nodeWakes judges one node.
func nodeWakes(n ast.Node, netName string, dot bool) string {
	switch n := n.(type) {
	case *ast.BasicLit:
		if s, err := strconv.Unquote(n.Value); n.Kind == token.STRING && err == nil && strings.Contains(strings.ToLower(s), "herdr") {
			return "names herdr in a string or an import: hosted mode never wakes a session"
		}
	case *ast.CompositeLit:
		if isNet(n.Type, "UnixAddr", netName, dot) {
			return "builds a net.UnixAddr: hosted mode never dials a session's socket"
		}
	case *ast.CallExpr:
		return callWakes(n, netName, dot)
	}
	return ""
}

func callWakes(c *ast.CallExpr, netName string, dot bool) string {
	if isNet(c.Fun, "DialUnix", netName, dot) {
		return "calls net.DialUnix: hosted mode never dials a session's socket"
	}
	if dialName(c.Fun, dot) && hasUnixNetwork(c.Args) {
		return "dials a unix socket: hosted mode never dials a session's socket"
	}
	return ""
}

// dialName says whether the callee is a .Dial, .DialContext or .DialTimeout selector, or a bare
// DialTimeout under a dot import of net.
func dialName(fun ast.Expr, dot bool) bool {
	switch f := fun.(type) {
	case *ast.SelectorExpr:
		return dialNames[f.Sel.Name]
	case *ast.Ident:
		return dot && f.Name == "DialTimeout"
	}
	return false
}

func hasUnixNetwork(args []ast.Expr) bool {
	for _, a := range args {
		if lit, ok := a.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if s, err := strconv.Unquote(lit.Value); err == nil && unixNetworks[s] {
				return true
			}
		}
	}
	return false
}

// isNet says whether e names net.<name>, through the import's own name, or bare under a dot import.
func isNet(e ast.Expr, name, netName string, dot bool) bool {
	switch e := e.(type) {
	case *ast.SelectorExpr:
		id, ok := e.X.(*ast.Ident)
		return ok && netName != "" && id.Name == netName && e.Sel.Name == name
	case *ast.Ident:
		return dot && e.Name == name
	}
	return false
}

// netImportName is the name the file imports net under ("" if it does not) and whether it is a
// dot import.
func netImportName(file *ast.File) (name string, dot bool) {
	for _, imp := range file.Imports {
		if p, err := strconv.Unquote(imp.Path.Value); err != nil || p != "net" {
			continue
		}
		switch {
		case imp.Name == nil:
			return "net", false
		case imp.Name.Name == ".":
			return "", true
		case imp.Name.Name != "_":
			return imp.Name.Name, false
		}
	}
	return "", false
}
