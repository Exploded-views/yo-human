//go:build !unix

package secrets

import (
	"io/fs"
	"os"
)

const openFlags = os.O_RDONLY

func isSymlinkError(error) bool { return false }

func ownedByMe(fs.FileInfo) bool { return true }
