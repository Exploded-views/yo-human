package main

import (
	"os"
	"testing"
)

// main passes Run's code to exit. A bad argument list makes Run return its usage code, 2, without
// touching a database, so main can be run here with exit stubbed.
func TestMainExitsWithRunsCode(t *testing.T) {
	oldArgs, oldExit := os.Args, exit
	t.Cleanup(func() { os.Args, exit = oldArgs, oldExit })
	os.Args = []string{"cmd", "bogus"}
	code := -1
	exit = func(c int) { code = c }
	main()
	if code != 2 {
		t.Errorf("main exited with %d, want 2", code)
	}
}
