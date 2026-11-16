package app

import (
	"errors"
	"time"
)

// Secret is a configuration value that must never reach output: every formatting verb prints
// a placeholder, and Reveal is the one way to the value.
type Secret string

// Reveal returns the secret's value.
func (s Secret) Reveal() string { return string(s) }

// Config is yo-human-hosted's configuration, read from the environment.
type Config struct {
	DBPath, PublicURL                                string
	OwnerGitHubID                                    int64
	GitHubClientID                                   string
	GitHubClientSecret                               Secret
	RedirectAllow, CIMDHosts, OriginAllow, LinkHosts []string
	BarkURL, BarkUser                                string
	BarkPassword, BarkDeviceKey                      Secret
	NudgeWindow                                      time.Duration
	NudgeMinSeverity, NudgeLevel                     string
	EscalateAfter                                    map[string]time.Duration
	WalledSwarms                                     []string
	EscalateCritical                                 bool
	AccessTTL, RefreshTTL, ToolDeadline              time.Duration
	WebAuthnRPID, ApproveOrigin                      string
	ChallengeTTL, DeskTTL                            time.Duration
	ListenMCP, ListenApprove                         string
}

// LoadConfig reads the configuration through env.
func LoadConfig(env func(string) string) (Config, error) {
	return Config{}, errors.New("stub")
}

// NormaliseHost lowercases host, drops a trailing dot and drops a default :443.
func NormaliseHost(host string) string { return host }

func dbPath(env func(string) string, flagValue string) string { return "" }
