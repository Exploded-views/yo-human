package core

// AfterEnqueue installs a hook that runs once the message is on disk and before any send, so a test
// can make a resolve land at exactly that point.
func AfterEnqueue(p *Pipeline, f func()) { p.afterEnqueue = f }

// AfterClose installs a hook that runs inside Resolve right after the incident is marked closed,
// while the incident lock is held.
func AfterClose(p *Pipeline, f func()) { p.afterClose = f }

// FingerprintOf exposes the destination fingerprint, so a test can queue an entry as the pipeline would.
var FingerprintOf = fingerprintOf

// AddReached exposes the merge of a delivery into an incident's reach records.
var AddReached = addReached

// BeforeLand installs a hook that runs in landed, after a send succeeded and before the incident
// lock is taken to record it.
func BeforeLand(p *Pipeline, f func()) { p.beforeLand = f }

// BeforeAdmit installs a hook that runs when a flush has claimed a message waiting for its incident
// check, before it takes the incident lock to check it.
func BeforeAdmit(p *Pipeline, f func()) { p.beforeAdmit = f }
