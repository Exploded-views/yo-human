package config

import (
	"fmt"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// keywordSchema uses each keyword the validator knows in shapes the config schemas do not: a
// const and an enum that hold arrays and objects, a list of types, a fractional minimum, a $ref
// inside items, and additionalProperties as a schema.
const keywordSchema = `{
  "type": "object",
  "properties": {
    "c": {"const": {"a": [1, 2.0, {"b": null}]}},
    "e": {"enum": [[1, "x"], {"k": true}, 1.0, "s", null, false]},
    "t": {"type": ["string", "null", "integer"]},
    "n": {"type": "number", "minimum": 0.5},
    "i": {"type": "array", "items": {"$ref": "#/$defs/item"}},
    "o": {"additionalProperties": {"type": "boolean"}, "propertyNames": {"minLength": 2}}
  },
  "$defs": {"item": {"type": "object", "required": ["id"], "properties": {"id": {"type": "integer", "minimum": 1}}}}
}`

var keywordDocs = []string{
	`{c: {a: [1, 2, {b: null}]}}`,
	`{c: {a: [1, 2, {b: 0}]}}`,
	`{c: {a: [1, 2]}}`,
	`{c: {a: [1, 2, {b: null}], z: 1}}`,
	`{c: {a: [1, 3, {b: null}]}}`,
	`{c: [1, 2]}`,
	`{c: 1}`,
	`{e: [1, x]}`,
	`{e: [1, y]}`,
	`{e: [1]}`,
	`{e: {k: true}}`,
	`{e: {k: false}}`,
	`{e: {k: true, j: 1}}`,
	`{e: 1}`,
	`{e: 1.5}`,
	`{e: s}`,
	`{e: null}`,
	`{e: false}`,
	`{e: true}`,
	`{t: x}`,
	`{t: null}`,
	`{t: 3}`,
	`{t: 3.0}`,
	`{t: 3.5}`,
	`{t: [x]}`,
	`{n: 0.5}`,
	`{n: 0.49}`,
	`{n: 1}`,
	`{n: -1}`,
	`{n: "1"}`,
	`{i: [{id: 1}, {id: 0}, {}, x, {id: 1.5}]}`,
	`{i: x}`,
	`{o: {ab: true, cd: 1, e: false}}`,
	`{o: x}`,
	`{o: {"ü": true, "üü": true}}`,
}

func TestValidatorAgreesWithJSONSchemaOnEveryKeywordShape(t *testing.T) {
	ref, err := refCompile([]byte(keywordSchema))
	if err != nil {
		t.Fatal(err)
	}
	own, err := compileNode([]byte(keywordSchema))
	if err != nil {
		t.Fatal(err)
	}
	p := schemaPair{"keywords", ref, own}
	for i, doc := range keywordDocs {
		var v any
		if err := yaml.Unmarshal([]byte(doc), &v); err != nil {
			t.Fatalf("doc %d: %v", i, err)
		}
		agree(t, p, fmt.Sprintf("doc %d %q", i, doc), v)
	}
}

// A schema the validator cannot check in full is refused, never checked in part.
func TestCompileNodeRefusesWhatItCannotCheck(t *testing.T) {
	for name, doc := range map[string]string{
		"not JSON":                 `{`,
		"not an object":            `[]`,
		"an unknown keyword":       `{"maxLength": 3}`,
		"a nested unknown keyword": `{"properties": {"a": {"oneOf": []}}}`,
		"a bad type":               `{"type": 5}`,
		"a type list of non-names": `{"type": ["string", 5]}`,
		"a bad required":           `{"required": "a"}`,
		"a bad enum":               `{"enum": "a"}`,
		"a bad pattern":            `{"pattern": "("}`,
		"a pattern not a string":   `{"pattern": 5}`,
		"a negative minLength":     `{"minLength": -1}`,
		"a minLength not a number": `{"minLength": "1"}`,
		"a minimum not a number":   `{"minimum": "1"}`,
		"properties not an object": `{"properties": []}`,
		"a property not a schema":  `{"properties": {"a": 5}}`,
		"items not a schema":       `{"items": true}`,
		"additional not a schema":  `{"additionalProperties": 5}`,
		"a $ref not a string":      `{"$ref": 5}`,
		"a $ref to nothing":        `{"$ref": "#/$defs/none"}`,
		"a $ref outside $defs":     `{"$ref": "other.json"}`,
		"a $ref to another tree":   `{"$ref": "#/definitions/item", "$defs": {"item": {}}}`,
		"$defs not an object":      `{"$defs": []}`,
		"a $defs entry not object": `{"$defs": {"a": 5}}`,
	} {
		if _, err := compileNode([]byte(doc)); err == nil {
			t.Errorf("%s: %s compiled; want it refused", name, doc)
		}
	}
}

func TestEveryShippedSchemaCompiles(t *testing.T) {
	for name, p := range map[string]string{"config": string(schemaJSON), "keywords": keywordSchema} {
		if _, err := compileNode([]byte(p)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if !strings.Contains(string(Schema()), `"$schema"`) {
		t.Error("the published schema lost its $schema")
	}
}
