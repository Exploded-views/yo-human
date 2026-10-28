package repolint

import (
	"os"
	"sort"
	"strings"
)

// Cgo returns the files under root's internal/ directory that import "C", sorted by path. It
// skips testdata/, .cache/ and dist/ directories and nested modules. golangci-lint lints
// cgo-processed files, where import "C" is gone, so depguard cannot deny cgo; this check does.
func Cgo(root string) ([]TagFinding, error) {
	files, err := goFiles(os.DirFS(root))
	if err != nil {
		return nil, err
	}
	var found []TagFinding
	for _, f := range files {
		if !strings.HasPrefix(f.path, "internal/") {
			continue
		}
		for _, ip := range f.imports {
			if ip == "C" {
				found = append(found, TagFinding{f.path, `imports "C": internal packages do not use cgo`})
			}
		}
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].Path < found[j].Path })
	return found, nil
}
