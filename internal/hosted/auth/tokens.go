// Package auth mints bus tokens and verifies the bearer a caller presents. It imports bus only
// (the principal and the credential rows) and makes no outbound call.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"fmt"
	"strings"
)

// The token prefixes. Task 2's gitleaks rule covers all six (yh[narhdx]_).
const (
	PrefixNode     = "yhn_" // a node, scoped to one swarm
	PrefixAccess   = "yha_" // an OAuth access token (Duet)
	PrefixRefresh  = "yhr_" // an OAuth refresh token
	PrefixStarter  = "yhd_" // a desk starter
	PrefixDesk     = "yhh_" // a desk human token
	PrefixExecutor = "yhx_" // the executor
)

// tokenBytes of crypto/rand give 52 base32 characters.
const (
	tokenBytes = 32
	bodyLength = 52
)

var (
	prefixes = []string{PrefixNode, PrefixAccess, PrefixRefresh, PrefixStarter, PrefixDesk, PrefixExecutor}
	encoding = base32.StdEncoding.WithPadding(base32.NoPadding)
)

// Mint returns a new token, its prefix and then 32 crypto/rand bytes in lowercase base32 (52
// characters), and the token's hash. prefix must be one of the six.
func Mint(prefix string) (token, hash string, err error) {
	if !validPrefix(prefix) {
		return "", "", fmt.Errorf("auth: %q is not a token prefix", prefix)
	}
	raw := make([]byte, tokenBytes)
	_, _ = rand.Read(raw) // crypto/rand.Read never returns an error: it ends the program instead
	token = prefix + strings.ToLower(encoding.EncodeToString(raw))
	return token, Hash(token), nil
}

// Hash is the SHA-256 of token in hex: the only form of a token the bus stores.
func Hash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func validPrefix(p string) bool {
	for _, q := range prefixes {
		if p == q {
			return true
		}
	}
	return false
}

// wellFormed reports whether token is a prefix, then 52 characters of lowercase base32.
func wellFormed(token string) bool {
	if len(token) != len(PrefixNode)+bodyLength || !validPrefix(token[:len(PrefixNode)]) {
		return false
	}
	for _, c := range []byte(token[len(PrefixNode):]) {
		if (c < 'a' || c > 'z') && (c < '2' || c > '7') {
			return false
		}
	}
	return true
}
