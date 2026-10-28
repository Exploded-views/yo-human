package bus

import (
	"crypto/rand"
	"strings"
)

// NewID returns prefix, "_", then 16 crypto/rand bytes in lowercase base32 (26 characters).
func NewID(prefix string) string {
	return prefix + "_" + strings.ToLower(rand.Text())
}
