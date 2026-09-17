package usecase

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/chck/synr/internal/domain"
)

type Clock interface {
	Now() time.Time
}

type Provider interface {
	List(context.Context) ([]domain.Conversation, error)
	Leave(context.Context, domain.Conversation) error
}

type Request struct {
	BeforeMonths int
	Apply        bool
	ProtectedIDs map[domain.ConversationID]struct{}
}

type Result struct {
	Decisions    []domain.Decision
	Left         []domain.Conversation
	Failed       *domain.Conversation
	NotAttempted []domain.Conversation
}

func Scan(ctx context.Context, provider Provider, request Request, clock Clock) (Result, error) {
	if request.BeforeMonths <= 0 {
		return Result{}, fmt.Errorf("before months must be greater than zero")
	}

	conversations, err := provider.List(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("list conversations: %w", err)
	}

	cutoff := clock.Now().AddDate(0, -request.BeforeMonths, 0)
	decisions := make([]domain.Decision, 0, len(conversations))
	for _, conversation := range conversations {
		decisions = append(decisions, domain.Evaluate(conversation, cutoff, request.ProtectedIDs))
	}
	sort.Slice(decisions, func(left int, right int) bool {
		leftConversation := decisions[left].Conversation()
		rightConversation := decisions[right].Conversation()
		if leftConversation.Name() != rightConversation.Name() {
			return leftConversation.Name() < rightConversation.Name()
		}
		if leftConversation.ID().Value() != rightConversation.ID().Value() {
			return leftConversation.ID().Value() < rightConversation.ID().Value()
		}
		return leftConversation.ID().Service() < rightConversation.ID().Service()
	})

	result := Result{Decisions: decisions}
	if !request.Apply {
		return result, nil
	}

	for index, decision := range decisions {
		if !decision.Eligible() {
			continue
		}
		conversation := decision.Conversation()
		if err := provider.Leave(ctx, conversation); err != nil {
			result.Failed = &conversation
			result.NotAttempted = eligibleConversations(decisions[index+1:])
			return result, fmt.Errorf("leave %s conversation %s: %w", conversation.Service(), conversation.ID().Value(), err)
		}
		result.Left = append(result.Left, conversation)
	}

	return result, nil
}

func eligibleConversations(decisions []domain.Decision) []domain.Conversation {
	conversations := make([]domain.Conversation, 0, len(decisions))
	for _, decision := range decisions {
		if decision.Eligible() {
			conversations = append(conversations, decision.Conversation())
		}
	}
	return conversations
}
