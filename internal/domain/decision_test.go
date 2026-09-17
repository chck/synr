package domain

import (
	"testing"
	"time"
)

func TestEvaluate(t *testing.T) {
	now := time.Date(2026, time.September, 18, 12, 0, 0, 0, time.UTC)
	cutoff := now.AddDate(0, -1, 0)
	old, _ := KnownActivity(cutoff.Add(-time.Second))
	recent, _ := KnownActivity(cutoff)
	tests := []struct {
		name       string
		activity   Activity
		protection Protection
		configured bool
		eligible   bool
		reason     DecisionReason
	}{
		{name: "old", activity: old, eligible: true, reason: ReasonInactive},
		{name: "cutoff is not old", activity: recent, reason: ReasonActive},
		{name: "unknown", activity: UnknownActivity(), reason: ReasonUnknownActivity},
		{name: "service protected", activity: old, protection: ProtectionGeneral, reason: ReasonServiceProtected},
		{name: "configured", activity: old, configured: true, reason: ReasonConfiguredProtected},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conversation, err := NewConversation(ServiceSlack, "C123", "general", tt.activity, tt.protection)
			if err != nil {
				t.Fatal(err)
			}
			protected := map[ConversationID]struct{}{}
			if tt.configured {
				protected[conversation.ID()] = struct{}{}
			}
			decision := Evaluate(conversation, cutoff, protected)
			if decision.Eligible() != tt.eligible || decision.Reason() != tt.reason {
				t.Fatalf("got eligible=%v reason=%q", decision.Eligible(), decision.Reason())
			}
		})
	}
}

func TestNewConversationRejectsEmptyID(t *testing.T) {
	activity, err := KnownActivity(time.Date(2026, time.September, 18, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewConversation(ServiceSlack, "", "general", activity, ProtectionNone); err == nil {
		t.Fatal("expected an empty ID to be rejected")
	}
}

func TestNewConversationRejectsEmptyName(t *testing.T) {
	activity, err := KnownActivity(time.Date(2026, time.September, 18, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewConversation(ServiceSlack, "C123", "", activity, ProtectionNone); err == nil {
		t.Fatal("expected an empty name to be rejected")
	}
}

func TestKnownActivityRejectsZeroTimestamp(t *testing.T) {
	if _, err := KnownActivity(time.Time{}); err == nil {
		t.Fatal("expected a zero timestamp to be rejected")
	}
}

func TestParseServiceRejectsUnsupportedValue(t *testing.T) {
	if _, err := ParseService("discord"); err == nil {
		t.Fatal("expected an unsupported service to be rejected")
	}
}

func TestParseServiceAcceptsSupportedValues(t *testing.T) {
	tests := []struct {
		input string
		want  Service
	}{
		{input: "slack", want: ServiceSlack},
		{input: "chatwork", want: ServiceChatwork},
		{input: "zulip", want: ServiceZulip},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			service, err := ParseService(tt.input)
			if err != nil {
				t.Fatal(err)
			}
			if service != tt.want {
				t.Fatalf("got %q, want %q", service, tt.want)
			}
		})
	}
}

func TestNewConversationPreservesValues(t *testing.T) {
	activityAt := time.Date(2026, time.September, 17, 12, 0, 0, 0, time.UTC)
	activity, err := KnownActivity(activityAt)
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := NewConversation(ServiceZulip, "stream-42", "general", activity, ProtectionPinned)
	if err != nil {
		t.Fatal(err)
	}
	if conversation.Service() != ServiceZulip || conversation.Name() != "general" || conversation.Protection() != ProtectionPinned {
		t.Fatal("conversation did not preserve its values")
	}
	if conversation.ID().Service() != ServiceZulip || conversation.ID().Value() != "stream-42" {
		t.Fatal("conversation ID did not preserve its service and value")
	}
	if !conversation.Activity().Known() || !conversation.Activity().At().Equal(activityAt) {
		t.Fatal("conversation did not preserve its activity")
	}
}

func TestNewConversationRejectsUnsupportedProtection(t *testing.T) {
	activity, err := KnownActivity(time.Date(2026, time.September, 18, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewConversation(ServiceSlack, "C123", "general", activity, Protection("custom")); err == nil {
		t.Fatal("expected an unsupported protection to be rejected")
	}
}

func TestNewConversationRejectsUnsupportedService(t *testing.T) {
	activity, err := KnownActivity(time.Date(2026, time.September, 18, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewConversation(Service("discord"), "C123", "general", activity, ProtectionNone); err == nil {
		t.Fatal("expected an unsupported service to be rejected")
	}
}
