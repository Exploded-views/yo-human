## Summary

<!-- What changed and why. Reference the issue (#123). Say which PR this stacks on, if any. -->

## PR Checklist

- [ ] RED proof recorded; mutation check done on the key line
- [ ] `gofmt`, `go vet`, `golangci-lint` pass
- [ ] `go test -race` passes; coverage floors held or raised
- [ ] Performance budgets held (none raised)
- [ ] `gitleaks` passes; no secrets, real hosts or client names
- [ ] CLI contract unchanged, or CHANGELOG plus version bump plus skill update
- [ ] Docs updated (README, CLAUDE.md, skill, setup Inputs)
- [ ] Security-minded review if secrets, inbound replies, channel isolation, or hosted auth, tokens, approvals or infrastructure were touched
- [ ] No principle violations, or each one justified in writing
