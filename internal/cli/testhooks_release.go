//go:build !e2e

package cli

// installTestHooks does nothing in a release build: the test-only clock and certificate hooks
// exist only under the e2e build tag (testhooks_e2e.go).
func installTestHooks() {}
