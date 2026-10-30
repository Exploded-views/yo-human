package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"
)

// schemaNode is one compiled JSON Schema, checked by hand: the binary carries no JSON Schema
// library, whose package init alone cost every command about 2ms (constitution VII). It knows the
// keywords the config schema and the channel settings schemas use, and refuses to compile any
// other, so a schema can never ask for a check that is silently skipped. The agreement tests hold
// it to the jsonschema library's answer, violation for violation. The checks are in schemacheck.go.
type schemaNode struct {
	types      []string
	hasConst   bool
	constVal   any
	enum       []any
	pattern    *regexp.Regexp
	minLength  int // -1: none
	minimum    *big.Rat
	required   []string
	properties map[string]*schemaNode
	noOthers   bool        // additionalProperties: false
	others     *schemaNode // additionalProperties: {schema}
	propNames  *schemaNode
	items      *schemaNode
	ref        string
	refNode    *schemaNode
}

// annotations are keywords that check nothing.
var annotations = map[string]bool{"$schema": true, "$id": true, "title": true, "description": true, "$defs": true}

// compileNode compiles a JSON Schema document. A keyword it does not know is an error.
func compileNode(doc []byte) (*schemaNode, error) {
	raw, err := decodeJSON(doc)
	if err != nil {
		return nil, err
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil, errors.New("a schema must be an object")
	}
	var refs []*schemaNode
	root, err := compileObj(obj, &refs)
	if err != nil {
		return nil, err
	}
	defs, err := compileDefs(obj["$defs"], &refs)
	if err != nil {
		return nil, err
	}
	for _, n := range refs {
		name, ok := strings.CutPrefix(n.ref, "#/$defs/")
		if n.refNode = defs[name]; !ok || n.refNode == nil {
			return nil, fmt.Errorf("$ref %q names no definition", n.ref)
		}
	}
	return root, nil
}

func compileDefs(v any, refs *[]*schemaNode) (map[string]*schemaNode, error) {
	defs := map[string]*schemaNode{}
	if v == nil {
		return defs, nil
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("$defs is not an object")
	}
	for _, name := range sortedKeys(obj) {
		sub, err := compileSub(obj[name], refs)
		if err != nil {
			return nil, fmt.Errorf("$defs/%s: %w", name, err)
		}
		defs[name] = sub
	}
	return defs, nil
}

// decodeJSON decodes one JSON value with its numbers kept exact.
func decodeJSON(b []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

type keywordCompiler func(n *schemaNode, v any, refs *[]*schemaNode) error

// compilers turn one keyword's value into the node's check. A keyword missing here is one this
// validator does not know. It is filled in init: its entries refer back to compileObj.
var compilers map[string]keywordCompiler

func init() {
	compilers = map[string]keywordCompiler{
		"type":                 compileType,
		"const":                func(n *schemaNode, v any, _ *[]*schemaNode) error { n.hasConst, n.constVal = true, v; return nil },
		"enum":                 compileEnum,
		"pattern":              compilePattern,
		"minLength":            compileMinLength,
		"minimum":              compileMinimum,
		"required":             compileRequired,
		"properties":           compileProperties,
		"additionalProperties": compileAdditional,
		"propertyNames": func(n *schemaNode, v any, refs *[]*schemaNode) (err error) {
			n.propNames, err = compileSub(v, refs)
			return
		},
		"items": func(n *schemaNode, v any, refs *[]*schemaNode) (err error) {
			n.items, err = compileSub(v, refs)
			return
		},
		"$ref": compileRef,
	}
}

func compileObj(obj map[string]any, refs *[]*schemaNode) (*schemaNode, error) {
	n := &schemaNode{minLength: -1}
	for _, k := range sortedKeys(obj) {
		if annotations[k] {
			continue
		}
		c, ok := compilers[k]
		if !ok {
			return nil, fmt.Errorf("unsupported schema keyword %q", k)
		}
		if err := c(n, obj[k], refs); err != nil {
			return nil, fmt.Errorf("%s: %w", k, err)
		}
	}
	return n, nil
}

func compileSub(v any, refs *[]*schemaNode) (*schemaNode, error) {
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("not a schema object")
	}
	return compileObj(obj, refs)
}

// stringList is v as a list of strings; a lone string is a list of one when single is true.
func stringList(v any, single bool) ([]string, error) {
	if s, ok := v.(string); ok && single {
		return []string{s}, nil
	}
	list, ok := v.([]any)
	if !ok {
		return nil, errors.New("not a list")
	}
	out := make([]string, 0, len(list))
	for _, e := range list {
		s, ok := e.(string)
		if !ok {
			return nil, errors.New("not a list of strings")
		}
		out = append(out, s)
	}
	return out, nil
}

func compileType(n *schemaNode, v any, _ *[]*schemaNode) (err error) {
	n.types, err = stringList(v, true)
	return err
}

func compileRequired(n *schemaNode, v any, _ *[]*schemaNode) (err error) {
	n.required, err = stringList(v, false)
	return err
}

func compileEnum(n *schemaNode, v any, _ *[]*schemaNode) error {
	list, ok := v.([]any)
	if !ok {
		return errors.New("not a list")
	}
	n.enum = list
	return nil
}

func compilePattern(n *schemaNode, v any, _ *[]*schemaNode) (err error) {
	s, ok := v.(string)
	if !ok {
		return errors.New("not a string")
	}
	n.pattern, err = regexp.Compile(s)
	return err
}

func compileMinLength(n *schemaNode, v any, _ *[]*schemaNode) error {
	num, _ := v.(json.Number)
	i, err := strconv.Atoi(string(num))
	if err != nil || i < 0 {
		return errors.New("not a non-negative integer")
	}
	n.minLength = i
	return nil
}

func compileMinimum(n *schemaNode, v any, _ *[]*schemaNode) error {
	r, ok := ratOf(v)
	if !ok {
		return errors.New("not a number")
	}
	n.minimum = r
	return nil
}

func compileProperties(n *schemaNode, v any, refs *[]*schemaNode) error {
	obj, ok := v.(map[string]any)
	if !ok {
		return errors.New("not an object")
	}
	n.properties = make(map[string]*schemaNode, len(obj))
	for _, k := range sortedKeys(obj) {
		sub, err := compileSub(obj[k], refs)
		if err != nil {
			return fmt.Errorf("%s: %w", k, err)
		}
		n.properties[k] = sub
	}
	return nil
}

func compileAdditional(n *schemaNode, v any, refs *[]*schemaNode) (err error) {
	if b, ok := v.(bool); ok {
		n.noOthers = !b
		return nil
	}
	n.others, err = compileSub(v, refs)
	return err
}

func compileRef(n *schemaNode, v any, refs *[]*schemaNode) error {
	s, ok := v.(string)
	if !ok {
		return errors.New("not a string")
	}
	n.ref = s
	*refs = append(*refs, n)
	return nil
}

func ratOf(v any) (*big.Rat, bool) {
	num, ok := v.(json.Number)
	if !ok {
		return nil, false
	}
	return new(big.Rat).SetString(string(num))
}
