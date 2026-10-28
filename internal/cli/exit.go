package cli

// Exit codes returned by Run. Usage errors use 64 (EX_USAGE from sysexits.h).
const (
	ExitOK    = 0
	ExitUsage = 64
)
