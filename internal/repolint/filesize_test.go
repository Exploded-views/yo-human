package repolint

import (
	"reflect"
	"testing"
)

const lineLimit = 400

func TestOversizedBoundary(t *testing.T) {
	// testdata/sizes/ok400.go has exactly 400 lines; big401.go has 401;
	// gen401.go has 401 and the "Code generated" header;
	// testdata/sizes/testdata/huge.go has 900.
	got, err := Oversized("testdata/sizes", lineLimit)
	if err != nil {
		t.Fatalf("Oversized: %v", err)
	}
	want := []Finding{{Path: "big401.go", Lines: 401}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Oversized = %v, want %v", got, want)
	}
}

func TestRepoWithinLimit(t *testing.T) {
	got, err := Oversized("../..", lineLimit)
	if err != nil {
		t.Fatalf("Oversized: %v", err)
	}
	for _, f := range got {
		t.Errorf("%s:%d exceeds %d lines", f.Path, f.Lines, lineLimit)
	}
}
