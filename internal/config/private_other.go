//go:build !unix

package config

import "io/fs"

func ownedByMe(fs.FileInfo) bool { return true }
