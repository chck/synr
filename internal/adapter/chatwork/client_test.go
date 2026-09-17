package chatwork

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/chck/synr/internal/domain"
	"github.com/chck/synr/internal/usecase"
)

var _ usecase.Provider = (*Client)(nil)

func TestListBuildsConversationsAndProtectsStickyAndDirectRooms(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", request.Method)
		}
		if request.URL.Path != "/rooms" {
			t.Errorf("path = %s, want /rooms", request.URL.Path)
		}
		if request.Header.Get("X-ChatWorkToken") != "test-token" {
			t.Errorf("X-ChatWorkToken = %q, want test-token", request.Header.Get("X-ChatWorkToken"))
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `[
			{"room_id": 101, "name": "old group", "type": "group", "sticky": false, "last_update_time": 1710000000},
			{"room_id": 102, "name": "sticky group", "type": "group", "sticky": true, "last_update_time": 1710000001},
			{"room_id": 103, "name": "direct room", "type": "direct", "sticky": false, "last_update_time": 1710000002},
			{"room_id": 104, "name": "unknown group", "type": "group", "sticky": false, "last_update_time": 0}
		]`)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	conversations, err := client.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if got, want := conversationIDs(conversations), []string{"101", "102", "103", "104"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("conversation IDs = %v, want %v", got, want)
	}
	if got, want := conversationProtections(conversations), []domain.Protection{
		domain.ProtectionNone,
		domain.ProtectionSticky,
		domain.ProtectionDirect,
		domain.ProtectionNone,
	}; !reflect.DeepEqual(got, want) {
		t.Errorf("protections = %v, want %v", got, want)
	}
	if got, want := conversations[0].Activity().At(), time.Unix(1710000000, 0); !conversations[0].Activity().Known() || !got.Equal(want) {
		t.Errorf("old group activity = (%v, %v), want known %v", conversations[0].Activity().Known(), got, want)
	}
	if conversations[3].Activity().Known() {
		t.Error("unknown group activity is known, want unknown")
	}
}

func TestListRejectsNonSuccessStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Error(writer, "not authorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	_, err := newTestClient(t, server).List(context.Background())
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("List() error = %v, want HTTP status error", err)
	}
}

func TestListRejectsMalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{`)
	}))
	defer server.Close()

	_, err := newTestClient(t, server).List(context.Background())
	if err == nil {
		t.Fatal("List() error = nil, want malformed JSON error")
	}
}

func TestListTreatsZeroTimestampAsUnknown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `[{"room_id": 105, "name": "empty", "type": "group", "sticky": false, "last_update_time": 0}]`)
	}))
	defer server.Close()

	conversations, err := newTestClient(t, server).List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(conversations) != 1 {
		t.Fatalf("conversation count = %d, want 1", len(conversations))
	}
	if conversations[0].Activity().Known() {
		t.Error("activity is known, want unknown")
	}
}

func TestListTreatsNegativeTimestampAsUnknown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `[{"room_id":106,"name":"invalid timestamp","type":"group","sticky":false,"last_update_time":-1}]`)
	}))
	defer server.Close()

	conversations, err := newTestClient(t, server).List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(conversations) != 1 {
		t.Fatalf("conversation count = %d, want 1", len(conversations))
	}
	if conversations[0].Activity().Known() {
		t.Error("negative timestamp activity is known, want unknown")
	}
}

func TestListRejectsIncompleteRoomData(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "room ID", body: `[{"name":"room","type":"group","sticky":false,"last_update_time":0}]`},
		{name: "name", body: `[{"room_id":106,"type":"group","sticky":false,"last_update_time":0}]`},
		{name: "type", body: `[{"room_id":106,"name":"room","sticky":false,"last_update_time":0}]`},
		{name: "sticky", body: `[{"room_id":106,"name":"room","type":"group","last_update_time":0}]`},
		{name: "last update time", body: `[{"room_id":106,"name":"room","type":"group","sticky":false}]`},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(writer, test.body)
			}))
			defer server.Close()

			_, err := newTestClient(t, server).List(context.Background())
			if err == nil {
				t.Fatal("List() error = nil, want incomplete room data error")
			}
		})
	}
}

func TestListRejectsTrailingJSON(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "malformed", body: `[] {`},
		{name: "additional value", body: `[] []`},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(writer, test.body)
			}))
			defer server.Close()

			_, err := newTestClient(t, server).List(context.Background())
			if err == nil {
				t.Fatal("List() error = nil, want trailing JSON error")
			}
		})
	}
}

func TestListRejectsNullJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `null`)
	}))
	defer server.Close()

	_, err := newTestClient(t, server).List(context.Background())
	if err == nil {
		t.Fatal("List() error = nil, want null payload error")
	}
}

func TestLeaveSendsFormEncodedAction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodDelete {
			t.Errorf("method = %s, want DELETE", request.Method)
		}
		if request.URL.EscapedPath() != "/rooms/room%2F123" {
			t.Errorf("escaped path = %s, want /rooms/room%%2F123", request.URL.EscapedPath())
		}
		if request.Header.Get("X-ChatWorkToken") != "test-token" {
			t.Errorf("X-ChatWorkToken = %q, want test-token", request.Header.Get("X-ChatWorkToken"))
		}
		if contentType := request.Header.Get("Content-Type"); contentType != "application/x-www-form-urlencoded" {
			t.Errorf("Content-Type = %q, want application/x-www-form-urlencoded", contentType)
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		form, err := url.ParseQuery(string(body))
		if err != nil {
			t.Fatal(err)
		}
		if action := form.Get("action_type"); action != "leave" {
			t.Errorf("action_type = %q, want leave", action)
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	if err := newTestClient(t, server).Leave(context.Background(), testConversation(t, "room/123")); err != nil {
		t.Fatal(err)
	}
}

func TestLeaveRejectsUnexpectedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Error(writer, "cannot leave", http.StatusOK)
	}))
	defer server.Close()

	err := newTestClient(t, server).Leave(context.Background(), testConversation(t, "123"))
	if err == nil || !strings.Contains(err.Error(), "200") {
		t.Fatalf("Leave() error = %v, want HTTP status error", err)
	}
}

func newTestClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	baseURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client, err := New("test-token", baseURL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func testConversation(t *testing.T, id string) domain.Conversation {
	t.Helper()
	conversation, err := domain.NewConversation(domain.ServiceChatwork, id, "test room", domain.UnknownActivity(), domain.ProtectionNone)
	if err != nil {
		t.Fatal(err)
	}
	return conversation
}

func conversationIDs(conversations []domain.Conversation) []string {
	ids := make([]string, 0, len(conversations))
	for _, conversation := range conversations {
		ids = append(ids, conversation.ID().Value())
	}
	return ids
}

func conversationProtections(conversations []domain.Conversation) []domain.Protection {
	protections := make([]domain.Protection, 0, len(conversations))
	for _, conversation := range conversations {
		protections = append(protections, conversation.Protection())
	}
	return protections
}
