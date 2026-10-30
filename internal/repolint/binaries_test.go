package repolint

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"strings"
	"testing"
)

func fakePE() []byte {
	b := make([]byte, 128)
	copy(b, "MZ")
	binary.LittleEndian.PutUint32(b[0x3c:], 0x40)
	copy(b[0x40:], "PE\x00\x00")
	return b
}

// A binary built at run time in a temp tree is found; text, late NULs and testdata are not.
func TestBinaryFilesFindsExecutablesAndNULs(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string][]byte{
		"perf":              append([]byte{0xcf, 0xfa, 0xed, 0xfe}, bytes.Repeat([]byte("x"), 100)...), // Mach-O 64, no NUL
		"macho32":           append([]byte{0xfe, 0xed, 0xfa, 0xce}, "abc"...),
		"fat":               append([]byte{0xca, 0xfe, 0xba, 0xbe}, "abc"...),
		"elf":               append([]byte("\x7fELF"), "abc"...),
		"tools/x.exe":       fakePE(),
		"data.bin":          []byte("abc\x00def"),
		"deep/dir/blob":     append(bytes.Repeat([]byte("a"), 7990), 0),
		"late-nul":          append(bytes.Repeat([]byte("a"), 8000), 0),
		"text.go":           []byte("package x\n"),
		"mz.txt":            []byte("MZ is not a header\n"),
		"testdata/a.bin":    []byte("a\x00b"),
		"pkg/testdata/b.so": append([]byte("\x7fELF"), 0),
		"mytestdata/c.bin":  []byte("a\x00b"),
	} {
		plant(t, root, name, string(content))
	}
	paths, err := RepoFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := BinaryFiles(root, paths)
	want := []string{"data.bin", "deep/dir/blob", "elf", "fat", "macho32", "mytestdata/c.bin", "perf", "tools/x.exe"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("BinaryFiles = %v, %v; want %v", got, err, want)
	}
	// Not binary: empty, a short MZ, an MZ whose offset leads nowhere.
	plant(t, root, "empty.txt", "")
	plant(t, root, "shortmz.txt", "MZ")
	badpe := fakePE()
	binary.LittleEndian.PutUint32(badpe[0x3c:], 0x7fffffff)
	plant(t, root, "badpe.dat", strings.ReplaceAll(string(badpe), "\x00", "z"))
	if got, err := BinaryFiles(root, []string{"empty.txt", "shortmz.txt", "badpe.dat"}); err != nil || len(got) != 0 {
		t.Errorf("not-binary files: %v, %v", got, err)
	}
	// A directory cannot be read as a file.
	if _, err := BinaryFiles(root, []string{"tools"}); err == nil {
		t.Error("no error reading a directory")
	}
	if _, err := BinaryFiles(root, []string{"missing"}); err == nil {
		t.Error("no error for a missing file")
	}
}

// A built binary at the repo root once rode in on a merge (round 6, HIGH-1): nothing executable
// or binary is committed outside testdata/.
func TestNoBinariesInRepo(t *testing.T) {
	paths, err := RepoFiles("../..")
	if err != nil {
		t.Fatal(err)
	}
	found, err := BinaryFiles("../..", paths)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range found {
		if !strings.HasPrefix(f, "testdata/") {
			t.Errorf("%s: a binary file (NUL byte or an executable header) outside testdata/", f)
		}
	}
}
