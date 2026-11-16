package bus

// eventReader turns the events of one page into updates, reading each item and thread once.
type eventReader struct {
	x     Tx
	p     Principal
	items map[string]*Item
	msgs  map[string][]Message
}

func (r *eventReader) item(id string) (*Item, error) {
	if it, ok := r.items[id]; ok {
		return it, nil
	}
	it, found, err := r.x.ItemByID(id)
	if err != nil {
		return nil, err
	}
	if !found {
		r.items[id] = nil
		return nil, nil
	}
	r.items[id] = &it
	return &it, nil
}

// mine reports whether the node itself wrote the item: what it did to its own items is not news to it.
func (r *eventReader) mine(it *Item) bool {
	return it != nil && it.FromKind == PrincipalNode && it.From == r.p.Name
}

// update is e as the node's update. ok is false for an event the node has no use for: its own
// doing, a status push, a relay, or a type that isn't an agent's concern.
func (r *eventReader) update(e Event) (u Update, ok bool, err error) {
	switch e.Type {
	case EventItemPosted, EventItemRevised, EventItemEscalated, EventApprovalVoided:
		return r.itemEvent(e)
	case EventMessageAdded:
		return r.messageEvent(e)
	case EventAnswerRecorded, EventApprovalRecorded, EventAnswerSuperseded:
		return r.recordEvent(e)
	}
	return Update{}, false, nil
}

func base(e Event, it *Item) Update {
	u := Update{Seq: e.Seq, Type: e.Type, ItemID: e.ItemID}
	if it != nil {
		u.Handle, u.Revision = it.Handle, it.Revision
	}
	return u
}

func (r *eventReader) itemEvent(e Event) (Update, bool, error) {
	it, err := r.item(e.ItemID)
	if err != nil || r.mine(it) {
		return Update{}, false, err
	}
	return base(e, it), true, nil
}

// messageEvent finds the message the event records: the thread's message made at the event's time.
func (r *eventReader) messageEvent(e Event) (Update, bool, error) {
	it, err := r.item(e.ItemID)
	if err != nil {
		return Update{}, false, err
	}
	msgs, ok := r.msgs[e.ItemID]
	if !ok {
		if msgs, err = r.x.Messages(e.ItemID); err != nil {
			return Update{}, false, err
		}
		r.msgs[e.ItemID] = msgs
	}
	u := base(e, it)
	for i := range msgs {
		if msgs[i].Created.Equal(e.Created) {
			if msgs[i].FromKind == PrincipalNode && msgs[i].From == r.p.Name {
				return Update{}, false, nil
			}
			m := msgs[i]
			u.Message = &m
			break
		}
	}
	return u, true, nil
}

// recordEvent attaches the answer or approval the event wrote: the item's record of the event's
// kind whose Recorded time is the event's Created time (the convention on Tx.DecisionRecorded).
// An event names no revision, so Revision is the record's own when one is found.
func (r *eventReader) recordEvent(e Event) (Update, bool, error) {
	it, err := r.item(e.ItemID)
	if err != nil {
		return Update{}, false, err
	}
	u := base(e, it)
	want := RecordAnswer
	if e.Type == EventApprovalRecorded {
		want = RecordApproval
	}
	rec, found, err := r.x.DecisionRecorded(e.ItemID, want, e.Created)
	if err != nil {
		return Update{}, false, err
	}
	if found {
		u.Record, u.Revision = &rec, rec.Revision
	}
	return u, true, nil
}
