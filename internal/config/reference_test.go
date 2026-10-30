package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
)

// The reference validator is the jsonschema library the binary used before its own validator.
// It lives only in tests, so the binary does not carry the library (constitution VII); the
// agreement tests hold the two to the same answer on every input.

// refCompile builds a reference validator from a JSON Schema document.
func refCompile(doc []byte) (*jsonschema.Schema, error) {
	v, err := jsonschema.UnmarshalJSON(strings.NewReader(string(doc)))
	if err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("mem:///schema.json", v); err != nil {
		return nil, err
	}
	return c.Compile("mem:///schema.json")
}

// refViolations validates value (any YAML-decoded tree) against sch, as the binary did.
func refViolations(sch *jsonschema.Schema, value any) ([]violation, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	inst, err := jsonschema.UnmarshalJSON(strings.NewReader(string(b)))
	if err != nil {
		return nil, err
	}
	err = sch.Validate(inst)
	if err == nil {
		return nil, nil
	}
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		return nil, err
	}
	var out []violation
	refCollect(ve, &out)
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out, nil
}

func refCollect(ve *jsonschema.ValidationError, out *[]violation) {
	if len(ve.Causes) > 0 {
		for _, c := range ve.Causes {
			refCollect(c, out)
		}
		return
	}
	parts := make([]string, len(ve.InstanceLocation))
	for i, p := range ve.InstanceLocation {
		parts[i] = escape(p)
	}
	path := strings.Join(parts, ".")
	if ap, ok := ve.ErrorKind.(*kind.AdditionalProperties); ok {
		for _, name := range ap.Properties {
			*out = append(*out, violation{join(path, escape(name)), "unknown key"})
		}
		return
	}
	kw := strings.Join(ve.ErrorKind.KeywordPath(), "/")
	if kw == "" {
		kw = "schema"
	}
	*out = append(*out, violation{path, fmt.Sprintf("fails %q", kw)})
}
