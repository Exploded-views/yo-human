package bus

import "unicode/utf8"

// maxPushBody is how much of Duet's text a push carries, in characters: a push is a notice, and
// the owner opens the item for the rest.
const maxPushBody = 300

// pushLevel is the level a push carries for a post of this severity, clamped to passive and
// active: no loud push goes out without an escalation. A low post is passive, the rest active.
func pushLevel(s Severity) string {
	if s == SeverityLow {
		return "passive"
	}
	return "active"
}

// clip is s cut to at most n characters, never in the middle of one.
func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n])
}

// queuePost queues the push a post carries to the owner, in the post's own transaction, so the
// owner is never told of an item that was not stored and an item is never stored without its
// push. Duet's post to the owner is its own message, with its title and summary. A node's page
// is a count only: item text stays off the push path.
func (b *Bus) queuePost(x Tx, p Principal, it Item) error {
	if it.To != "human" {
		return nil
	}
	row := b.pushRow(it, it.Severity)
	switch {
	case p.Kind == PrincipalDuet:
		row.Kind, row.Title, row.Body = OutboxDuetPost, clip(it.Title, MaxTitle), clip(it.Summary, maxPushBody)
	case it.Urgency == UrgencyPage:
		row.Kind, row.Title, row.Body = OutboxPage, "yo-human", "1 page for Claude"
	default:
		return nil
	}
	_, err := x.InsertOutbox(row)
	return err
}

// queueReply is queuePost for a message in the thread of an item addressed to the owner.
func (b *Bus) queueReply(x Tx, p Principal, it Item, body string, sev Severity, urg Urgency) error {
	if it.To != "human" {
		return nil
	}
	row := b.pushRow(it, sev)
	switch {
	case p.Kind == PrincipalDuet && b.l.NoDuetReplyPush:
		return nil
	case p.Kind == PrincipalDuet:
		row.Kind, row.Title, row.Body = OutboxDuetPost, "yo-human", clip(body, maxPushBody)
	case p.Kind == PrincipalNode && urg == UrgencyPage:
		row.Kind, row.Title, row.Body = OutboxPage, "yo-human", "1 page for Claude"
	default:
		return nil
	}
	_, err := x.InsertOutbox(row)
	return err
}

// pushRow is a pending row for item it, due now; the caller sets its kind and text.
func (b *Bus) pushRow(it Item, sev Severity) OutboxRow {
	now := b.now().UTC()
	return OutboxRow{Swarm: it.Swarm, ItemID: it.ID, Revision: it.Revision, Level: pushLevel(sev), State: OutboxPending, NextAt: now, Created: now}
}
