package repolint

// LineFinding is one line of a file that breaks a line rule.
type LineFinding struct {
	Path string
	Line int
}

// CodenameMapLines is a stub; the RED commit fails on its behaviour.
func CodenameMapLines(root string, paths []string) ([]LineFinding, error) { return nil, nil }

// TrackedFiles is a stub too.
func TrackedFiles(root string) ([]string, error) { return nil, nil }
