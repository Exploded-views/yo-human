package config

import (
	_ "embed"
	"strconv"
)

//go:embed schema.json
var schemaJSON []byte

// Schema returns the JSON Schema every config file is validated against. It is the published
// contract; schemanode.go checks a file against it by hand.
func Schema() []byte { return append([]byte(nil), schemaJSON...) }

// violation is one schema failure: where, and which keyword. It never quotes the value, because a
// rejected instance may be a secret.
type violation struct{ Path, Msg string }

// String renders "path: message", with the top level named as such.
func (v violation) String() string { return orRoot(v.Path) + ": " + v.Msg }

func join(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}

func orRoot(path string) string {
	if path == "" {
		return "(top level)"
	}
	return path
}

// escape makes a key from the file safe to print: control characters become escapes.
func escape(s string) string {
	q := strconv.Quote(s)
	return q[1 : len(q)-1]
}
