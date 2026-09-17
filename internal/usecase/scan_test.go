package usecase

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/chck/synr/internal/domain"
)

var scanNow = time.Date(2026, time.September, 18, 12, 0, 0, 0, time.UTC)

type fixedClock struct {
	now time.Time
}

func (clock fixedClock) Now() time.Time {
	return clock.now
}

type recordingProvider struct {
	conversations []domain.Conversation
	listErr       error
	leaveErrs     map[domain.ConversationID]error
	leaveCalls    []domain.Conversation
	listCalls     int
}

func (provider *recordingProvider) List(context.Context) ([]domain.Conversation, error) {
	provider.listCalls++
	if provider.listErr != nil {
		return nil, provider.listErr
	}
	return provider.conversations, nil
}

func (provider *recordingProvider) Leave(_ context.Context, conversation domain.Conversation) error {
	provider.leaveCalls = append(provider.leaveCalls, conversation)
	return provider.leaveErrs[conversation.ID()]
}

func TestScanDoesNotLeaveInPreview(t *testing.T) {
	provider := &recordingProvider{conversations: []domain.Conversation{oldConversation(t, "alpha", "alpha")}}

	result, err := Scan(context.Background(), provider, Request{BeforeMonths: 1}, fixedClock{now: scanNow})

	if err != nil {
		t.Fatal(err)
	}
	if provider.listCalls != 1 {
		t.Fatalf("got %d list calls, want 1", provider.listCalls)
	}
	if len(provider.leaveCalls) != 0 {
		t.Fatalf("got leave calls %v, want none", conversationNames(provider.leaveCalls))
	}
	if len(result.Decisions) != 1 || !result.Decisions[0].Eligible() {
		t.Fatalf("got decisions %#v, want one eligible decision", result.Decisions)
	}
	if len(result.Left) != 0 || result.Failed != nil || len(result.NotAttempted) != 0 {
		t.Fatalf("got preview result %#v, want no apply outcomes", result)
	}
}

func TestScanDoesNotLeaveWhenListFails(t *testing.T) {
	listErr := errors.New("provider unavailable")
	provider := &recordingProvider{listErr: listErr}

	result, err := Scan(context.Background(), provider, Request{BeforeMonths: 1, Apply: true}, fixedClock{now: scanNow})

	if !errors.Is(err, listErr) {
		t.Fatalf("got error %v, want wrapped %v", err, listErr)
	}
	if err == nil || err.Error() != "list conversations: provider unavailable" {
		t.Fatalf("got error %v, want list context", err)
	}
	if provider.listCalls != 1 {
		t.Fatalf("got %d list calls, want 1", provider.listCalls)
	}
	if len(provider.leaveCalls) != 0 {
		t.Fatalf("got leave calls %v, want none", conversationNames(provider.leaveCalls))
	}
	if len(result.Decisions) != 0 || len(result.Left) != 0 || result.Failed != nil || len(result.NotAttempted) != 0 {
		t.Fatalf("got result %#v, want empty result", result)
	}
}

func TestScanSortsDecisionsByNameThenID(t *testing.T) {
	provider := &recordingProvider{conversations: []domain.Conversation{
		oldConversation(t, "3", "beta"),
		oldConversation(t, "2", "alpha"),
		oldConversation(t, "1", "alpha"),
	}}

	result, err := Scan(context.Background(), provider, Request{BeforeMonths: 1}, fixedClock{now: scanNow})

	if err != nil {
		t.Fatal(err)
	}
	if got, want := decisionIDs(result.Decisions), []string{"1", "2", "3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got decision IDs %v, want %v", got, want)
	}
}

func TestScanAppliesOnlyEligibleConversations(t *testing.T) {
	old := oldConversation(t, "old", "old")
	active := conversation(t, "active", "active", scanNow, domain.ProtectionNone)
	unknown := unknownConversation(t, "unknown", "unknown")
	protected := oldConversation(t, "protected", "protected")
	provider := &recordingProvider{conversations: []domain.Conversation{old, active, unknown, protected}}

	result, err := Scan(context.Background(), provider, Request{
		BeforeMonths: 1,
		Apply:        true,
		ProtectedIDs: map[domain.ConversationID]struct{}{protected.ID(): {}},
	}, fixedClock{now: scanNow})

	if err != nil {
		t.Fatal(err)
	}
	if got, want := conversationNames(provider.leaveCalls), []string{"old"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got leave calls %v, want %v", got, want)
	}
	if got, want := conversationNames(result.Left), []string{"old"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got left %v, want %v", got, want)
	}
	if result.Failed != nil || len(result.NotAttempted) != 0 {
		t.Fatalf("got apply result %#v, want no failure", result)
	}
}

func TestScanStopsAfterFirstLeaveFailure(t *testing.T) {
	alpha := oldConversation(t, "alpha", "alpha")
	beta := oldConversation(t, "beta", "beta")
	gamma := oldConversation(t, "gamma", "gamma")
	leaveErr := errors.New("permission denied")
	provider := &recordingProvider{
		conversations: []domain.Conversation{gamma, beta, alpha},
		leaveErrs: map[domain.ConversationID]error{
			beta.ID(): leaveErr,
		},
	}

	result, err := Scan(context.Background(), provider, Request{BeforeMonths: 1, Apply: true}, fixedClock{now: scanNow})

	if !errors.Is(err, leaveErr) {
		t.Fatalf("got error %v, want wrapped %v", err, leaveErr)
	}
	if err == nil || err.Error() != "leave slack conversation beta: permission denied" {
		t.Fatalf("got error %v, want leave context", err)
	}
	if got, want := conversationNames(provider.leaveCalls), []string{"alpha", "beta"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got leave calls %v, want %v", got, want)
	}
	if got, want := conversationNames(result.Left), []string{"alpha"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got left %v, want %v", got, want)
	}
	if result.Failed == nil || result.Failed.Name() != "beta" {
		t.Fatalf("got failed %#v, want beta", result.Failed)
	}
	if got, want := conversationNames(result.NotAttempted), []string{"gamma"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got not attempted %v, want %v", got, want)
	}
}

func TestScanRejectsNonPositiveMonths(t *testing.T) {
	for _, beforeMonths := range []int{0, -1} {
		t.Run("before months", func(t *testing.T) {
			provider := &recordingProvider{}

			_, err := Scan(context.Background(), provider, Request{BeforeMonths: beforeMonths}, fixedClock{now: scanNow})

			if err == nil || err.Error() != "before months must be greater than zero" {
				t.Fatalf("got error %v, want non-positive month validation", err)
			}
			if provider.listCalls != 0 {
				t.Fatalf("got %d list calls, want 0", provider.listCalls)
			}
		})
	}
}

func oldConversation(t *testing.T, id string, name string) domain.Conversation {
	t.Helper()
	return conversation(t, id, name, scanNow.AddDate(0, -2, 0), domain.ProtectionNone)
}

func unknownConversation(t *testing.T, id string, name string) domain.Conversation {
	t.Helper()
	conversation, err := domain.NewConversation(domain.ServiceSlack, id, name, domain.UnknownActivity(), domain.ProtectionNone)
	if err != nil {
		t.Fatal(err)
	}
	return conversation
}

func conversation(t *testing.T, id string, name string, at time.Time, protection domain.Protection) domain.Conversation {
	t.Helper()
	activity, err := domain.KnownActivity(at)
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := domain.NewConversation(domain.ServiceSlack, id, name, activity, protection)
	if err != nil {
		t.Fatal(err)
	}
	return conversation
}

func decisionIDs(decisions []domain.Decision) []string {
	ids := make([]string, 0, len(decisions))
	for _, decision := range decisions {
		ids = append(ids, decision.Conversation().ID().Value())
	}
	return ids
}

func conversationNames(conversations []domain.Conversation) []string {
	names := make([]string, 0, len(conversations))
	for _, conversation := range conversations {
		names = append(names, conversation.Name())
	}
	return names
}
