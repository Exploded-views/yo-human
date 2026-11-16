package repolint

import (
	"reflect"
	"testing"
)

func TestPasskeyKindFixtures(t *testing.T) {
	// testdata/passkey: the bus and approval packages may name the kind; db names the identifier,
	// web converts the string (bare and through a package), a test file outside uses KindPasskey,
	// and web/fine.go uses the string "passkey" for something else.
	got, err := PasskeyKind("testdata/passkey")
	if err != nil {
		t.Fatalf("PasskeyKind: %v", err)
	}
	var paths []string
	for _, f := range got {
		paths = append(paths, f.Path)
	}
	want := []string{"internal/hosted/db/bad_ident.go", "internal/hosted/web/bad_conv.go", "internal/hosted/web/bad_pkgconv.go", "internal/hosted/web/bad_test.go"}
	if !reflect.DeepEqual(paths, want) {
		t.Errorf("PasskeyKind paths = %v, want %v", paths, want)
	}
}

// TestPasskeyKindOnlyInApproval: the plan names KindPasskey; the identifier on main is
// PrincipalPasskey, and the check covers both.
func TestPasskeyKindOnlyInApproval(t *testing.T) {
	got, err := PasskeyKind("../..")
	if err != nil {
		t.Fatalf("PasskeyKind: %v", err)
	}
	for _, f := range got {
		t.Errorf("%s: %s", f.Path, f.Msg)
	}
}
