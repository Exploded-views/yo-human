package registry_test

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"testing"
)

const (
	registryPath  = "github.com/Exploded-views/yo-human/internal/channel/registry"
	interfacePath = "github.com/Exploded-views/yo-human/internal/channel"
)

// The registry's exported API names only internal/channel types. A re-exported implementation
// type, or an alias of a standard library one (ntfy's *http.Client, say), would hand the cli a
// way around the network rule that depguard cannot see: the cli imports only the registry.
func TestRegistryExposesOnlyChannelTypes(t *testing.T) {
	pkg, err := importer.ForCompiler(token.NewFileSet(), "source", nil).Import(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	scope := pkg.Scope()
	exported := 0
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		if !obj.Exported() {
			continue
		}
		exported++
		if _, ok := obj.(*types.TypeName); ok {
			t.Errorf("registry exports the type %s; it may export only functions and values of internal/channel types", name)
			continue
		}
		for _, bad := range foreignTypes(obj.Type(), map[types.Type]bool{}) {
			t.Errorf("registry.%s exposes %s, which is not an internal/channel type", name, bad)
		}
	}
	if exported == 0 {
		t.Error("registry exports nothing; the check proves nothing")
	}
}

// foreignTypes lists the named types in t that belong to a package other than internal/channel,
// the builtin error, and every interface (any, interface{}, an inline one). It does not look
// inside internal/channel's own types.
func foreignTypes(t types.Type, seen map[types.Type]bool) []string {
	if seen[t] {
		return nil
	}
	seen[t] = true
	switch x := t.(type) {
	case *types.Alias:
		return named(x.Obj(), x)
	case *types.Named:
		return named(x.Obj(), x)
	case *types.Signature:
		return append(tupleTypes(x.Params(), seen), tupleTypes(x.Results(), seen)...)
	case *types.Struct:
		var out []string
		for i := range x.NumFields() {
			out = append(out, foreignTypes(x.Field(i).Type(), seen)...)
		}
		return out
	case *types.Interface:
		return []string{x.String()} // an inline interface holds anything that has its methods
	}
	var out []string
	for _, e := range elems(t) {
		out = append(out, foreignTypes(e, seen)...)
	}
	return out
}

// elems is what a pointer, slice, array, channel or map holds; nothing for a basic type.
func elems(t types.Type) []types.Type {
	switch x := t.(type) {
	case *types.Pointer:
		return []types.Type{x.Elem()}
	case *types.Slice:
		return []types.Type{x.Elem()}
	case *types.Array:
		return []types.Type{x.Elem()}
	case *types.Chan:
		return []types.Type{x.Elem()}
	case *types.Map:
		return []types.Type{x.Key(), x.Elem()}
	}
	return nil
}

// named passes an internal/channel type and fails any other, the builtin error included: an
// interface other than internal/channel's holds a value of any type that has its methods.
func named(obj *types.TypeName, t types.Type) []string {
	if obj.Pkg() != nil && obj.Pkg().Path() == interfacePath {
		return nil
	}
	return []string{t.String()}
}

func tupleTypes(tup *types.Tuple, seen map[types.Type]bool) []string {
	var out []string
	for i := range tup.Len() {
		out = append(out, foreignTypes(tup.At(i).Type(), seen)...)
	}
	return out
}

// L3: an interface type other than the internal/channel ones hands out anything (the holder
// type-asserts it back to an *http.Client), so it fails the check wherever it appears: any,
// interface{}, error, or an inline interface in a field, map, parameter or result.
func TestForeignTypesRejectsInterfaces(t *testing.T) {
	src := `package probe

import "github.com/Exploded-views/yo-human/internal/channel"

var Kinds = map[string]any{}
var Empty interface{}
var Field struct{ X interface{ M() } }
func Param(x any) {}
func Result() error { return nil }
func Slice() []interface{} { return nil }

var Reg channel.Registry
var Count int
func Name(s string) (string, channel.Type) { return s, channel.Type{} }
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "probe.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	conf := types.Config{Importer: importer.ForCompiler(fset, "source", nil)}
	pkg, err := conf.Check("example.com/probe", fset, []*ast.File{f}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"Kinds": true, "Empty": true, "Field": true, "Param": true, "Result": true, "Slice": true}
	for _, name := range pkg.Scope().Names() {
		got := len(foreignTypes(pkg.Scope().Lookup(name).Type(), map[types.Type]bool{})) > 0
		if got != want[name] {
			t.Errorf("%s: flagged %v, want %v", name, got, want[name])
		}
	}
}
