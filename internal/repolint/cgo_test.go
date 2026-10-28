package repolint

import (
	"reflect"
	"testing"
)

func TestCgoFixtures(t *testing.T) {
	// testdata/cgo: internal/store/x.go imports "C"; internal/core does not; tools/ok/y.go
	// imports "C" outside internal/, which is allowed.
	got, err := Cgo("testdata/cgo")
	if err != nil {
		t.Fatalf("Cgo: %v", err)
	}
	want := []TagFinding{{"internal/store/x.go", `imports "C": internal packages do not use cgo`}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Cgo = %v, want %v", got, want)
	}
}

func TestRepoCgo(t *testing.T) {
	got, err := Cgo("../..")
	if err != nil {
		t.Fatalf("Cgo: %v", err)
	}
	for _, f := range got {
		t.Errorf("%s: %s", f.Path, f.Msg)
	}
}
