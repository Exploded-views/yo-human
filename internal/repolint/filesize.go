// Package repolint holds repository-wide checks that go test enforces.
package repolint

import (
	"bytes"
	"io/fs"
	"os"
	"regexp"
	"sort"
	"strings"
)

// Finding is a Go file that exceeds the line limit.
type Finding struct {
	Path  string
	Lines int
}

var generatedHeader = regexp.MustCompile(`^// Code generated .* DO NOT EDIT\.$`)

var skipDirs = map[string]bool{"testdata": true, ".cache": true, "dist": true}

// Oversized returns the Go files under root with more than limit lines,
// sorted by path relative to root. It skips testdata/, .cache/ and dist/
// directories and files whose first line is a "Code generated" header.
func Oversized(root string, limit int) ([]Finding, error) {
	fsys := os.DirFS(root)
	var found []Finding
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != "." && skipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, err := checkFile(fsys, path, limit)
		if f != nil {
			found = append(found, *f)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(found, func(i, j int) bool { return found[i].Path < found[j].Path })
	return found, nil
}

// checkFile returns a Finding when the file is over the limit and not generated.
func checkFile(fsys fs.FS, path string, limit int) (*Finding, error) {
	data, err := fs.ReadFile(fsys, path)
	if err != nil {
		return nil, err
	}
	lines := countLines(data)
	if lines <= limit || isGenerated(data) {
		return nil, nil
	}
	return &Finding{Path: path, Lines: lines}, nil
}

// countLines counts newlines, plus one for a final line without a newline.
func countLines(data []byte) int {
	n := bytes.Count(data, []byte("\n"))
	if len(data) > 0 && data[len(data)-1] != '\n' {
		n++
	}
	return n
}

func isGenerated(data []byte) bool {
	first, _, _ := bytes.Cut(data, []byte("\n"))
	return generatedHeader.Match(bytes.TrimSuffix(first, []byte("\r")))
}
