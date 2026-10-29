package bus

import "errors"

// visible reports whether p may see it. The owner's kinds (duet, passkey and desk) see every
// swarm; a node sees only its own, and only if that is a valid swarm (not "duet" or "human");
// any other kind sees nothing. It switches on p.Kind and never treats an empty Swarm as "all":
// the owner's credentials are recognised by kind alone.
func (b *Bus) visible(p Principal, it Item) bool {
	switch p.Kind {
	case PrincipalDuet, PrincipalDesk, PrincipalPasskey:
		return true
	case PrincipalNode:
		return ValidSwarm(p.Swarm) && it.Swarm == p.Swarm
	default:
		return false
	}
}

// notFoundErr is the one error for an item that is missing and for one the caller may not see.
// Both must give the same code and the same text, so a caller learns nothing about other swarms.
func notFoundErr() *Error {
	return &Error{Code: CodeNotFound, What: "item", Valid: "the id of an item in your swarm", Fix: "check the id and read your swarm's items again"}
}

// mayWrite says who can post and reply: a node with a valid swarm, and Duet. The desk only
// decides; the passkey, executor and desk-start credentials never write items.
func mayWrite(p Principal) *Error {
	switch {
	case p.Kind == PrincipalDuet:
		return nil
	case p.Kind == PrincipalNode && ValidSwarm(p.Swarm) && p.Name != "":
		return nil
	}
	return &Error{Code: CodeForbidden, What: "credential", Valid: "a node credential or Duet's",
		Fix: "use a node credential to post to your swarm"}
}

// isWalled reports whether swarm is behind the client wall. The set comes from Limits at New.
func (b *Bus) isWalled(swarm string) bool {
	_, ok := b.walled[swarm]
	return ok
}

// checkSwarmContent is the one place a swarm's content rules run for a post, once, before
// anything is stored. For a walled swarm, Duet's post may only be go or hold, and a node's is
// typed lines (CheckWalledDraft); any other swarm is unchecked.
func (b *Bus) checkSwarmContent(p Principal, swarm string, d Draft) error {
	switch {
	case !b.isWalled(swarm):
		return nil
	case p.Kind == PrincipalDuet:
		return checkIntoWalledDraft(d)
	}
	return CheckWalledDraft(d)
}

// checkSwarmMessage is the same hook for a reply. Reply calls it after the visibility check,
// with the item's own swarm, so a caller who can't see the item gets not_found before the wall
// can say anything about it.
func (b *Bus) checkSwarmMessage(p Principal, swarm, body string) error {
	if !b.isWalled(swarm) {
		return nil
	}
	return CheckWalledMessage(body, p.Kind == PrincipalDuet)
}

// mapStoreErr turns what a Store returns into a bus error. A bus error passes through; the
// store's not-found is the same error as a missing id; a stale revision is stale_revision; a
// kind mismatch (the bus bumps the revision with any kind change, so it means a bug) and
// everything else is internal, with no detail of the cause.
func mapStoreErr(err error) error {
	switch {
	case err == nil:
		return nil
	case asBus(err):
		return err
	case errors.Is(err, ErrNotFound):
		return notFoundErr()
	case errors.Is(err, ErrStaleRevision):
		return &Error{Code: CodeStaleRevision, What: "revision", Valid: "the item's current revision", Fix: "read the item and post again"}
	}
	return &Error{Code: CodeInternal, What: "the bus", Valid: "a retry", Fix: "try again; if it keeps failing, tell the owner", cause: err}
}

func asBus(err error) bool {
	_, ok := AsError(err)
	return ok
}
