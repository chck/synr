package domain

import "time"

type DecisionReason string

const (
	ReasonInactive            DecisionReason = "inactive"
	ReasonActive              DecisionReason = "active"
	ReasonUnknownActivity     DecisionReason = "unknown activity"
	ReasonServiceProtected    DecisionReason = "service protected"
	ReasonConfiguredProtected DecisionReason = "configured protected"
)

type Decision struct {
	conversation Conversation
	eligible     bool
	reason       DecisionReason
}

func Evaluate(conversation Conversation, cutoff time.Time, protected map[ConversationID]struct{}) Decision {
	if _, configured := protected[conversation.ID()]; configured {
		return ineligibleDecision(conversation, ReasonConfiguredProtected)
	}
	if conversation.Protection() != ProtectionNone {
		return ineligibleDecision(conversation, ReasonServiceProtected)
	}
	activity := conversation.Activity()
	if !activity.Known() {
		return ineligibleDecision(conversation, ReasonUnknownActivity)
	}
	if activity.At().Before(cutoff) {
		return Decision{conversation: conversation, eligible: true, reason: ReasonInactive}
	}
	return ineligibleDecision(conversation, ReasonActive)
}

func (decision Decision) Conversation() Conversation {
	return decision.conversation
}

func (decision Decision) Eligible() bool {
	return decision.eligible
}

func (decision Decision) Reason() DecisionReason {
	return decision.reason
}

func ineligibleDecision(conversation Conversation, reason DecisionReason) Decision {
	return Decision{conversation: conversation, reason: reason}
}
