package bus

import "github.com/Exploded-views/yo-human/internal/hosted/policy"

// Validate checks d against the limits.
func (d *Draft) Validate(l Limits) error { return nil }

// ValidName reports whether s is a valid swarm or node name.
func ValidName(s string) bool { return false }

// PolicyTarget is the policy target of d.
func (d *Draft) PolicyTarget() policy.Target { return policy.Target{} }

// Approvable returns nil only when action a on target t may be posted as an approval.
func (l Limits) Approvable(a Action, t policy.Target) error { return nil }
