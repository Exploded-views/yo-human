package repolint

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"strings"
)

// sniffLen is how much of a file is read to decide whether it is binary.
const sniffLen = 8000

// executableMagics are the leading bytes of the executables a build leaves behind: Mach-O (32 and
// 64 bit, both byte orders), a universal Mach-O (also the Java class magic), and ELF. A PE file is
// recognised separately, by its MZ header pointing at a PE signature.
var executableMagics = [][]byte{
	{0xfe, 0xed, 0xfa, 0xce}, {0xfe, 0xed, 0xfa, 0xcf}, {0xce, 0xfa, 0xed, 0xfe}, {0xcf, 0xfa, 0xed, 0xfe},
	{0xca, 0xfe, 0xba, 0xbe}, []byte("\x7fELF"),
}

// BinaryFiles returns the named files (paths relative to root) that are binary: a NUL byte in the
// first 8000 bytes, or a Mach-O, ELF or PE executable header. Files under a testdata/ directory
// are exempt, since fixtures may hold binary data. Built output does not belong in the repo: it
// embeds the builder's paths. Findings are in the order of paths.
func BinaryFiles(root string, paths []string) ([]string, error) {
	var found []string
	for _, p := range paths {
		if strings.HasPrefix(p, "testdata/") || strings.Contains(p, "/testdata/") {
			continue
		}
		head, err := readHead(root, p)
		if err != nil {
			return nil, err
		}
		if bytes.IndexByte(head, 0) >= 0 || isExecutable(head) {
			found = append(found, p)
		}
	}
	return found, nil
}

func readHead(root, p string) ([]byte, error) {
	f, err := os.DirFS(root).Open(p)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, sniffLen)
	n, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, err // a short or empty file is fine
	}
	return buf[:n], nil
}

func isExecutable(head []byte) bool {
	for _, m := range executableMagics {
		if bytes.HasPrefix(head, m) {
			return true
		}
	}
	if len(head) >= 0x40 && bytes.HasPrefix(head, []byte("MZ")) {
		off := int(binary.LittleEndian.Uint32(head[0x3c:]))
		return off+4 <= len(head) && bytes.Equal(head[off:off+4], []byte("PE\x00\x00"))
	}
	return false
}
