package config

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"unicode/utf8"
)

// nodeViolations validates value (any YAML-decoded tree) against n. The value goes through JSON
// first, as the library's input did, so a structure JSON cannot express is an error.
func nodeViolations(n *schemaNode, value any) ([]violation, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	inst, err := decodeJSON(b)
	if err != nil {
		return nil, err
	}
	var out []violation
	n.check(inst, "", &out)
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out, nil
}

// check appends every violation of v, found at path, to out.
func (n *schemaNode) check(v any, path string, out *[]violation) {
	fail := func(kw string) { *out = append(*out, violation{path, fmt.Sprintf("fails %q", kw)}) }
	if n.refNode != nil {
		n.refNode.check(v, path, out)
	}
	if len(n.types) > 0 && !n.typeMatches(v) {
		fail("type")
	}
	if n.hasConst && !jsonEqual(v, n.constVal) {
		fail("const")
	}
	if n.enum != nil && !n.inEnum(v) {
		fail("enum")
	}
	n.checkKind(v, path, out, fail)
}

// checkKind runs the keywords that apply to v's JSON type only.
func (n *schemaNode) checkKind(v any, path string, out *[]violation, fail func(string)) {
	switch x := v.(type) {
	case string:
		n.checkString(x, fail)
	case json.Number:
		if r, ok := ratOf(x); ok && n.minimum != nil && r.Cmp(n.minimum) < 0 {
			fail("minimum")
		}
	case map[string]any:
		n.checkObject(x, path, out, fail)
	case []any:
		if n.items != nil {
			for i, e := range x {
				n.items.check(e, join(path, strconv.Itoa(i)), out)
			}
		}
	}
}

func (n *schemaNode) checkString(s string, fail func(string)) {
	if n.minLength >= 0 && utf8.RuneCountInString(s) < n.minLength {
		fail("minLength")
	}
	if n.pattern != nil && !n.pattern.MatchString(s) {
		fail("pattern")
	}
}

func (n *schemaNode) checkObject(obj map[string]any, path string, out *[]violation, fail func(string)) {
	for _, r := range n.required {
		if _, ok := obj[r]; !ok {
			fail("required")
			break
		}
	}
	for _, k := range sortedKeys(obj) {
		if n.propNames != nil {
			// The library reports a property name at the top level, wherever the object is; so
			// does this, so a config check says the same as before.
			n.propNames.check(k, "", out)
		}
		at := join(path, escape(k))
		if sub, ok := n.properties[k]; ok {
			sub.check(obj[k], at, out)
			continue
		}
		if n.noOthers {
			*out = append(*out, violation{at, "unknown key"})
		} else if n.others != nil {
			n.others.check(obj[k], at, out)
		}
	}
}

// typeMatches reports whether v is one of the node's JSON types. An integer is a number with no
// fraction, however it is spelled, and every integer is a number.
func (n *schemaNode) typeMatches(v any) bool {
	for _, t := range n.types {
		if got := jsonType(v); got == t || t == "number" && got == "integer" {
			return true
		}
	}
	return false
}

func jsonType(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	case json.Number:
		if r, ok := ratOf(x); ok && r.IsInt() {
			return "integer"
		}
	}
	return "number"
}

func (n *schemaNode) inEnum(v any) bool {
	for _, e := range n.enum {
		if jsonEqual(v, e) {
			return true
		}
	}
	return false
}

// jsonEqual is JSON equality: numbers by value, arrays and objects element by element.
func jsonEqual(a, b any) bool {
	switch x := a.(type) {
	case json.Number:
		ra, ok1 := ratOf(x)
		rb, ok2 := ratOf(b)
		return ok1 && ok2 && ra.Cmp(rb) == 0
	case []any:
		y, ok := b.([]any)
		return ok && arraysEqual(x, y)
	case map[string]any:
		y, ok := b.(map[string]any)
		return ok && objectsEqual(x, y)
	}
	return a == b
}

func arraysEqual(x, y []any) bool {
	if len(x) != len(y) {
		return false
	}
	for i := range x {
		if !jsonEqual(x[i], y[i]) {
			return false
		}
	}
	return true
}

func objectsEqual(x, y map[string]any) bool {
	if len(x) != len(y) {
		return false
	}
	for k, xv := range x {
		if yv, ok := y[k]; !ok || !jsonEqual(xv, yv) {
			return false
		}
	}
	return true
}
