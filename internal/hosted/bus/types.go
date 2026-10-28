// Package bus is hosted mode's domain: items, their validation, and the rules for answers and
// approvals. Standard library only, plus the owner's policy.
package bus

import (
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/policy"
)

// Kind is what an item asks of its reader.
type Kind string

// The item kinds.
const (
	KindApproval Kind = "approval"
	KindQuestion Kind = "question"
	KindBlocker  Kind = "blocker"
	KindFinished Kind = "finished"
	KindFailed   Kind = "failed"
	KindNotice   Kind = "notice"
)

// Severity orders items for the reader.
type Severity string

// The severities.
const (
	SeverityLow      Severity = "low"
	SeverityNormal   Severity = "normal"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

// Action is the category of what an approval authorizes. ActionNone is a plain item.
type Action string

// The actions: none, the policy's nine categories, and other.
const (
	ActionNone       Action = "none"
	ActionMerge      Action = "merge"
	ActionRelease    Action = "release"
	ActionDeploy     Action = "deploy"
	ActionPublicSync Action = "public-sync"
	ActionForcePush  Action = "force-push"
	ActionDelete     Action = "delete"
	ActionSpend      Action = "spend"
	ActionSecrets    Action = "secrets"
	ActionMoney      Action = "money"
	ActionOther      Action = "other"
)

// PrincipalKind is the kind of credential a caller presented.
type PrincipalKind string

// The principal kinds. PrincipalDesk and PrincipalDeskStart exist only under desk Option A.
const (
	PrincipalDuet      PrincipalKind = "duet"
	PrincipalNode      PrincipalKind = "node"
	PrincipalPasskey   PrincipalKind = "passkey"
	PrincipalExecutor  PrincipalKind = "executor"
	PrincipalDesk      PrincipalKind = "desk"
	PrincipalDeskStart PrincipalKind = "desk_start"
)

// Principal is the authenticated caller. Swarm is empty for Duet and the desk.
type Principal struct {
	Kind        PrincipalKind
	Name, Swarm string
}

// ActorKind records whether a human or an agent made an answer or approval.
type ActorKind string

// The actor kinds.
const (
	ActorHuman ActorKind = "human"
	ActorAgent ActorKind = "agent"
)

// ActorKind is the only place an actor kind is decided, and only from the credential: human
// for a passkey assertion and, under desk Option A, the desk; agent for everything else.
func (p Principal) ActorKind() ActorKind {
	switch p.Kind {
	case PrincipalPasskey, PrincipalDesk:
		return ActorHuman
	default:
		return ActorAgent
	}
}

// Draft is an item as a caller submits it. Every field is untrusted data.
type Draft struct {
	Kind                               Kind
	Severity                           Severity
	To, Handle, Title, Summary, Detail string
	RequiredAction, Repo, Ref, BaseRef string
	Target, PinnedCommit, PinnedTree   string
	Action                             Action
	Link                               string
	RiskFlags                          []string
	Untrusted                          bool
}

// State is an item's lifecycle state.
type State string

// The states.
const (
	StateOpen    State = "open"
	StateDecided State = "decided"
)

// Item is a stored draft.
type Item struct {
	Draft
	ID, Swarm, From   string
	FromKind          PrincipalKind
	Revision          int
	State             State
	ContentHash       string
	LastSeq, AckedSeq int64
	Created, Updated  time.Time
}

// RecordKind says whether a record answers a question or decides an approval.
type RecordKind string

// The record kinds.
const (
	RecordAnswer   RecordKind = "answer"
	RecordApproval RecordKind = "approval"
)

// Decision is an approval's outcome.
type Decision string

// The decisions.
const (
	DecisionApprove Decision = "approve"
	DecisionReject  Decision = "reject"
)

// Record is an answer or an approval for one item revision (stored from Task 4 on).
type Record struct {
	ItemID                          string
	Revision                        int
	Kind                            RecordKind
	Decision                        Decision
	ActorKind                       ActorKind
	Text, Source, By, SourceMessage string
	HumanTS, Recorded               time.Time
}

// The fixed limits (mechanisms, not policy).
const (
	MaxHandle         = 80       // characters
	MaxTitle          = 200      // characters
	MaxSummary        = 1000     // characters
	MaxRequiredAction = 1000     // characters
	MaxTarget         = 200      // characters
	MaxRef            = 100      // characters
	MaxDetail         = 16 << 10 // bytes
	MaxAnswer         = 4 << 10  // bytes
	MaxRiskFlags      = 8
	MaxLink           = 2048     // bytes
	MaxBody           = 64 << 10 // bytes
)

// Limits are the deployment's settings for validation and approval.
type Limits struct {
	LinkHosts []string
	Policy    policy.Policy
}
