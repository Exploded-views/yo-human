package secrets

import "os"

// SetOpenFile replaces how the secrets file is opened until the returned func runs.
func SetOpenFile(f func(string, int, os.FileMode) (*os.File, error)) (restore func()) {
	old := openFile
	openFile = f
	return func() { openFile = old }
}

// SetStatOpen replaces how the open secrets file is inspected until the returned func runs.
func SetStatOpen(f func(*os.File) (os.FileInfo, error)) (restore func()) {
	old := statOpen
	statOpen = f
	return func() { statOpen = old }
}
