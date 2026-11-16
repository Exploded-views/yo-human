// Package auth mints bus tokens and verifies the bearer a caller presents. It imports bus only
// (the principal and the credential rows) and makes no outbound call.
package auth

import "errors"

// The token prefixes. Task 2's gitleaks rule covers all six (yh[narhdx]_).
const (
	PrefixNode      = "yhn_"
	PrefixAccess    = "yha_"
	PrefixRefresh   = "yhr_"
	PrefixStarter   = "yhd_"
	PrefixDesk      = "yhh_"
	PrefixExecutor  = "yhx_"
	TokenBytes      = 32
	TokenBodyLength = 52
)

// Mint returns a new token with prefix and its SHA-256 hash in hex.
func Mint(prefix string) (token, hash string, err error) {
	return "", "", errors.New("stub")
}

// Hash is the SHA-256 of token in hex: the only form of a token the bus stores.
func Hash(token string) string { return "" }
