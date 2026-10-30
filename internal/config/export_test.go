package config

import "os"

// SetOpenFile replaces how config files are opened until the returned func runs.
func SetOpenFile(f func(string, int, os.FileMode) (*os.File, error)) (restore func()) {
	old := openFile
	openFile = f
	return func() { openFile = old }
}

// SetStatOpen replaces how an open config file is inspected until the returned func runs.
func SetStatOpen(f func(*os.File) (os.FileInfo, error)) (restore func()) {
	old := statOpen
	statOpen = f
	return func() { statOpen = old }
}

// SetExplicitGap runs f between the open of the explicit file and its checks, until restore runs.
func SetExplicitGap(f func()) (restore func()) {
	explicitGap = f
	return func() { explicitGap = nil }
}

// SetMine replaces the check that a file or directory belongs to the user until restore runs.
func SetMine(f func(os.FileInfo) bool) (restore func()) {
	old := isMine
	isMine = f
	return func() { isMine = old }
}
