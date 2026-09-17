package domain

import (
	"fmt"
	"time"
)

type Protection string

const (
	ProtectionNone    Protection = ""
	ProtectionGeneral Protection = "general"
	ProtectionSticky  Protection = "sticky"
	ProtectionDirect  Protection = "direct"
	ProtectionMy      Protection = "my"
	ProtectionPinned  Protection = "pinned"
)

type Activity struct {
	known bool
	at    time.Time
}

func KnownActivity(at time.Time) (Activity, error) {
	if at.IsZero() {
		return Activity{}, fmt.Errorf("known activity timestamp must not be zero")
	}
	return Activity{known: true, at: at}, nil
}

func UnknownActivity() Activity {
	return Activity{}
}

func (activity Activity) Known() bool {
	return activity.known
}

func (activity Activity) At() time.Time {
	return activity.at
}

type ConversationID struct {
	service Service
	value   string
}

func (id ConversationID) Service() Service {
	return id.service
}

func (id ConversationID) Value() string {
	return id.value
}

type Conversation struct {
	service    Service
	id         ConversationID
	name       string
	activity   Activity
	protection Protection
}

func NewConversation(service Service, id string, name string, activity Activity, protection Protection) (Conversation, error) {
	if !isSupportedService(service) {
		return Conversation{}, fmt.Errorf("unsupported service %q", service)
	}
	if id == "" {
		return Conversation{}, fmt.Errorf("conversation ID must not be empty")
	}
	if name == "" {
		return Conversation{}, fmt.Errorf("conversation name must not be empty")
	}
	if !isSupportedProtection(protection) {
		return Conversation{}, fmt.Errorf("unsupported protection %q", protection)
	}
	return Conversation{
		service:    service,
		id:         ConversationID{service: service, value: id},
		name:       name,
		activity:   activity,
		protection: protection,
	}, nil
}

func (conversation Conversation) Service() Service {
	return conversation.service
}

func (conversation Conversation) ID() ConversationID {
	return conversation.id
}

func (conversation Conversation) Name() string {
	return conversation.name
}

func (conversation Conversation) Activity() Activity {
	return conversation.activity
}

func (conversation Conversation) Protection() Protection {
	return conversation.protection
}

func isSupportedProtection(protection Protection) bool {
	switch protection {
	case ProtectionNone, ProtectionGeneral, ProtectionSticky, ProtectionDirect, ProtectionMy, ProtectionPinned:
		return true
	default:
		return false
	}
}
