package store

import (
	"os"
	"strings"
	"testing"
)

func writeState(t *testing.T, s *Store, body string) {
	t.Helper()
	if err := os.WriteFile(s.path("incidents.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readStateOf(t *testing.T, s *Store) (*State, error) {
	t.Helper()
	var got *State
	err := s.WithState(func(st *State) error { got = st; return nil })
	return got, err
}

// The counter hands out each generation once, and survives in the file, whatever is pruned.
func TestGenerationsAreNeverReused(t *testing.T) {
	s := open(t)
	var a, b int
	if err := s.WithState(func(st *State) error {
		a = st.NewGeneration()
		st.Incidents["k"] = &Incident{Key: "k", Generation: a}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.WithState(func(st *State) error {
		delete(st.Incidents, "k") // pruned
		b = st.NewGeneration()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	st, err := readStateOf(t, s)
	if a != 1 || b != 2 || err != nil || st.NextGeneration != 3 {
		t.Errorf("generations %d then %d, next %d, %v; want 1, 2, next 3", a, b, st.NextGeneration, err)
	}
}

// A file written before the counter, a bare map of incidents, is read, and the counter starts past
// every generation it holds; a counter behind the incidents is moved past them too.
func TestStateReadsTheOlderFileAndKeepsTheCounterAhead(t *testing.T) {
	s := open(t)
	writeState(t, s, `{"k": {"key": "k", "generation": 4, "open": true}, "n": null}`)
	st, err := readStateOf(t, s)
	if err != nil || st.Incidents["k"].Generation != 4 || st.NextGeneration != 5 {
		t.Fatalf("older file: %+v, %v; want k at 4, next 5", st, err)
	}
	writeState(t, s, `{"next_generation": 2, "incidents": {"k": {"key": "k", "generation": 7}, "n": null}}`)
	st, err = readStateOf(t, s)
	if err != nil || st.NextGeneration != 8 || len(st.Incidents) != 1 {
		t.Errorf("counter behind: %+v, %v; want next 8 and the null entry dropped", st, err)
	}
	writeState(t, s, `{"next_generation": 3}`)
	st, err = readStateOf(t, s)
	if err != nil || st.NextGeneration != 3 || st.Incidents == nil {
		t.Errorf("no incidents: %+v, %v; want next 3 and an empty map", st, err)
	}
}

func TestStateRefusesAFileItCannotParse(t *testing.T) {
	s := open(t)
	for _, body := range []string{
		`{"k": 3}`,
		`{"next_generation": "x", "incidents": {}}`,
		`{"next_generation": 1, "incidents": []}`,
	} {
		writeState(t, s, body)
		if _, err := readStateOf(t, s); err == nil || !strings.Contains(err.Error(), "cannot parse") {
			t.Errorf("%s: err = %v; want cannot parse", body, err)
		}
	}
}
