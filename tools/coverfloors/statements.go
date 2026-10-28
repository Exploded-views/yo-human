package main

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// HasStatements reports whether the non-test Go files in dir contain any
// function body or function literal. A package with none (only constants,
// variables and types) has nothing for a coverage profile to measure.
func HasStatements(dir string) (bool, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return false, err
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			return false, err
		}
		if declaresCode(f) {
			return true, nil
		}
	}
	return false, nil
}

func declaresCode(f *ast.File) bool {
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncDecl:
			found = found || x.Body != nil
		case *ast.FuncLit:
			found = true
		}
		return !found
	})
	return found
}

// DropStatementless removes from pkgs the packages that have no coverage and
// no statements. dirOf maps an import path to its directory.
func DropStatementless(cov map[string]float64, pkgs []string, dirOf func(string) (string, error)) ([]string, error) {
	var out []string
	for _, p := range pkgs {
		if _, ok := cov[p]; ok {
			out = append(out, p)
			continue
		}
		dir, err := dirOf(p)
		if err != nil {
			return nil, err
		}
		has, err := HasStatements(dir)
		if err != nil {
			return nil, err
		}
		if has {
			out = append(out, p)
		}
	}
	return out, nil
}

// moduleDir maps an import path to its directory in the module in the
// current directory, reading the module path from go.mod.
func moduleDir(p string) (string, error) {
	b, err := os.ReadFile("go.mod")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(b), "\n") {
		if mod, ok := strings.CutPrefix(line, "module "); ok {
			rel := strings.TrimPrefix(strings.TrimPrefix(p, strings.TrimSpace(mod)), "/")
			return filepath.FromSlash(rel), nil
		}
	}
	return "", errors.New("go.mod has no module line")
}
