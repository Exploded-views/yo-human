package bus

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"testing"
)

func fullDraft() Draft {
	return Draft{
		Kind: KindApproval, Severity: SeverityHigh, To: "human", Handle: "h", Title: "T", Summary: "S", Detail: "D",
		RequiredAction: "R", Repo: "o/r", Ref: "7", BaseRef: "main", Target: "o/r#7", PinnedCommit: "c", PinnedTree: "t",
		Action: ActionMerge, Link: "https://github.com/x", RiskFlags: []string{"a", "b"}, Untrusted: true,
	}
}

// The bytes the hash covers are pinned: the tags, their order and their names are the hash's
// version 1, independent of Draft's own field names and of any field added to Draft later.
func TestHashBytesArePinned(t *testing.T) {
	const want = `{"kind":"approval","severity":"high","to":"human","handle":"h","title":"T","summary":"S","detail":"D",` +
		`"required_action":"R","repo":"o/r","ref":"7","base_ref":"main","target":"o/r#7","pinned_commit":"c",` +
		`"pinned_tree":"t","action":"merge","link":"https://github.com/x","risk_flags":["a","b"],"untrusted":true}`
	if got := string(hashBytes(fullDraft())); got != want {
		t.Errorf("hash bytes =\n%s\nwant\n%s", got, want)
	}
	sum := sha256.Sum256([]byte(want))
	if got := contentHash(fullDraft()); got != "v1:"+hex.EncodeToString(sum[:]) {
		t.Errorf("contentHash = %s", got)
	}
	if string(hashBytes(Draft{})) != string(hashBytes(Draft{RiskFlags: []string{}})) {
		t.Error("nil and empty risk flags hash differently")
	}
}

// Every Draft field must be hashed. A new Draft field fails this test until it is added to
// hashDraft (and the pin above), so a content field can't silently fall outside the hash.
func TestHashDraftCoversEveryDraftField(t *testing.T) {
	var have []string
	for _, f := range reflect.VisibleFields(reflect.TypeFor[hashDraft]()) {
		have = append(have, f.Name)
	}
	var want []string
	for _, f := range reflect.VisibleFields(reflect.TypeFor[Draft]()) {
		want = append(want, f.Name)
	}
	if !reflect.DeepEqual(have, want) {
		t.Errorf("hashDraft fields %v, want Draft's %v", have, want)
	}
}
