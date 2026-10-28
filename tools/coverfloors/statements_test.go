package main

import "testing"

func TestHasStatements(t *testing.T) {
	for dir, want := range map[string]bool{"testdata/nostmt": false, "testdata/stmt": true} {
		got, err := HasStatements(dir)
		if err != nil || got != want {
			t.Errorf("%s: got %v, %v; want %v", dir, got, err, want)
		}
	}
}

func TestDropStatementlessKeepsMeasuredAndCodeBearing(t *testing.T) {
	dirOf := func(p string) (string, error) { return "testdata/" + p, nil }
	cov := map[string]float64{"measured": 50}
	got, err := DropStatementless(cov, []string{"measured", "nostmt", "stmt"}, dirOf)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "measured" || got[1] != "stmt" {
		t.Fatalf("got %v, want [measured stmt]: statementless dropped, unmeasured code kept", got)
	}
}
