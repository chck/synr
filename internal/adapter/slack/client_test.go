package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/chck/synr/internal/domain"
	"github.com/chck/synr/internal/usecase"
)

var _ usecase.Provider = (*Client)(nil)

type fixedClock struct{}

func (fixedClock) Now() time.Time {
	return time.Date(2026, time.September, 18, 0, 0, 0, 0, time.UTC)
}

func TestApplyUsesLatestMessageInsteadOfLastRead(t *testing.T) {
	leaves := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/conversations.list":
			writeJSON(t, writer, `{"ok":true,"channels":[{"id":"C1","is_member":true}]}`)
		case "/conversations.info":
			writeJSON(t, writer, `{"ok":true,"channel":{"id":"C1","name":"active","is_general":false,"last_read":"1710000000.000000"}}`)
		case "/conversations.history":
			if got := formValue(t, request, "channel"); got != "C1" {
				t.Errorf("history channel = %q, want C1", got)
			}
			if got := formValue(t, request, "limit"); got != "1" {
				t.Errorf("history limit = %q, want 1", got)
			}
			if formValue(t, request, "oldest") != "" || formValue(t, request, "latest") != "" {
				t.Error("history must request the newest message without time bounds")
			}
			writeJSON(t, writer, `{"ok":true,"messages":[{"ts":"1789689599.000000"}],"has_more":true}`)
		case "/conversations.leave":
			leaves++
			writeJSON(t, writer, `{"ok":true}`)
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	result, err := usecase.Scan(context.Background(), newTestClient(t, server), usecase.Request{BeforeMonths: 1, Apply: true}, fixedClock{})
	if err != nil {
		t.Fatal(err)
	}
	if leaves != 0 {
		t.Errorf("leave count = %d, want 0 for recent message with old last_read", leaves)
	}
	if len(result.Decisions) != 1 || result.Decisions[0].Reason() != domain.ReasonActive {
		t.Fatalf("decisions = %v, want one active conversation", result.Decisions)
	}
	if got := result.Decisions[0].Conversation().Activity().At(); !got.Equal(time.Unix(1789689599, 0)) {
		t.Errorf("activity = %v, want latest message timestamp", got)
	}
}

func TestNewRejectsInvalidConfiguration(t *testing.T) {
	for _, input := range []struct {
		name       string
		token      string
		httpClient *http.Client
		apiURL     string
	}{
		{name: "blank token", token: "   ", httpClient: http.DefaultClient, apiURL: "https://slack.example/"},
		{name: "nil HTTP client", token: "token", apiURL: "https://slack.example/"},
		{name: "blank API URL", token: "token", httpClient: http.DefaultClient, apiURL: "   "},
	} {
		t.Run(input.name, func(t *testing.T) {
			client, err := New(input.token, input.httpClient, input.apiURL)
			if err == nil {
				t.Errorf("New() = %v, nil, want configuration error", client)
			}
		})
	}
}

func TestListPaginatesMemberConversationsAndFetchesInfo(t *testing.T) {
	var infoCalls []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/conversations.list":
			if got, want := formValue(t, request, "limit"), "200"; got != want {
				t.Errorf("limit = %q, want %q", got, want)
			}
			if got, want := formValue(t, request, "exclude_archived"), "true"; got != want {
				t.Errorf("exclude_archived = %q, want %q", got, want)
			}
			if got, want := formValue(t, request, "types"), "public_channel,private_channel"; got != want {
				t.Errorf("types = %q, want %q", got, want)
			}
			switch formValue(t, request, "cursor") {
			case "":
				writeJSON(t, writer, `{"ok":true,"channels":[{"id":"C1","is_member":true},{"id":"C2","is_member":false}],"response_metadata":{"next_cursor":"second-page"}}`)
			case "second-page":
				writeJSON(t, writer, `{"ok":true,"channels":[{"id":"C3","is_member":true}],"response_metadata":{"next_cursor":""}}`)
			default:
				t.Errorf("cursor = %q, want a known page cursor", formValue(t, request, "cursor"))
				writer.WriteHeader(http.StatusBadRequest)
			}
		case "/conversations.info":
			id := formValue(t, request, "channel")
			infoCalls = append(infoCalls, id)
			switch id {
			case "C1":
				writeJSON(t, writer, `{"ok":true,"channel":{"id":"C1","name":"first","is_general":false,"last_read":"1789718400.000000"}}`)
			case "C3":
				writeJSON(t, writer, `{"ok":true,"channel":{"id":"C3","name":"third","is_general":false,"last_read":"1789718401.000000"}}`)
			default:
				t.Errorf("conversation info requested for %q, want a member conversation", id)
				writer.WriteHeader(http.StatusBadRequest)
			}
		case "/conversations.history":
			switch formValue(t, request, "channel") {
			case "C1":
				writeJSON(t, writer, `{"ok":true,"messages":[{"ts":"1789718400.000000"}]}`)
			case "C3":
				writeJSON(t, writer, `{"ok":true,"messages":[{"ts":"1789718401.000000"}]}`)
			default:
				t.Error("history requested for a non-member conversation")
				writer.WriteHeader(http.StatusBadRequest)
			}
		default:
			t.Errorf("path = %q, want a Slack Conversations endpoint", request.URL.Path)
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	conversations, err := newTestClient(t, server).List(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if got, want := infoCalls, []string{"C1", "C3"}; !reflect.DeepEqual(got, want) {
		t.Errorf("conversation info calls = %v, want %v", got, want)
	}
	if got, want := conversationIDs(conversations), []string{"C1", "C3"}; !reflect.DeepEqual(got, want) {
		t.Errorf("conversation IDs = %v, want %v", got, want)
	}
	if got, want := conversationNames(conversations), []string{"first", "third"}; !reflect.DeepEqual(got, want) {
		t.Errorf("conversation names = %v, want %v", got, want)
	}
	for index, timestamp := range []int64{1789718400, 1789718401} {
		activity := conversations[index].Activity()
		if !activity.Known() || !activity.At().Equal(time.Unix(timestamp, 0)) {
			t.Errorf("conversation %d activity = (%v, %v), want known %v", index, activity.Known(), activity.At(), time.Unix(timestamp, 0))
		}
	}
}

func TestListProtectsGeneralChannel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/conversations.list":
			writeJSON(t, writer, `{"ok":true,"channels":[{"id":"C1","is_member":true}],"response_metadata":{"next_cursor":""}}`)
		case "/conversations.info":
			writeJSON(t, writer, `{"ok":true,"channel":{"id":"C1","name":"general","is_general":true,"last_read":"1789718400.000000"}}`)
		case "/conversations.history":
			writeJSON(t, writer, `{"ok":true,"messages":[{"ts":"1710000000.000000"}]}`)
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	conversations, err := newTestClient(t, server).List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := conversations[0].Protection(), domain.ProtectionGeneral; got != want {
		t.Errorf("protection = %q, want %q", got, want)
	}
	if decision := domain.Evaluate(conversations[0], fixedClock{}.Now(), nil); decision.Eligible() {
		t.Error("general channel must remain protected with old messages")
	}
}

func TestListTreatsMissingOrMalformedMessageTimestampAsUnknown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/conversations.list":
			writeJSON(t, writer, `{"ok":true,"channels":[{"id":"C1","is_member":true},{"id":"C2","is_member":true},{"id":"C3","is_member":true},{"id":"C4","is_member":true}],"response_metadata":{"next_cursor":""}}`)
		case "/conversations.info":
			id := formValue(t, request, "channel")
			writeJSON(t, writer, fmt.Sprintf(`{"ok":true,"channel":{"id":%q,"name":%q,"is_general":false,"last_read":"1710000000.000000"}}`, id, id))
		case "/conversations.history":
			switch formValue(t, request, "channel") {
			case "C1":
				writeJSON(t, writer, `{"ok":true,"messages":[{}]}`)
			case "C2":
				writeJSON(t, writer, `{"ok":true,"messages":[{"ts":"not-a-timestamp"}]}`)
			case "C3":
				writeJSON(t, writer, `{"ok":true,"messages":[{"ts":"1789718400.not-a-fraction"}]}`)
			case "C4":
				writeJSON(t, writer, `{"ok":true,"messages":[{"ts":"1789718400."}]}`)
			}
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	conversations, err := newTestClient(t, server).List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, conversation := range conversations {
		if conversation.Activity().Known() {
			t.Errorf("conversation %q activity is known, want unknown", conversation.Name())
		}
	}
}

func TestListFailsOnPaginationError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/conversations.list" {
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		writeJSON(t, writer, `{"ok":false,"error":"ratelimited"}`)
	}))
	defer server.Close()

	conversations, err := newTestClient(t, server).List(context.Background())
	if err == nil {
		t.Fatal("List() error = nil, want Slack API error")
	}
	if conversations != nil {
		t.Errorf("List() conversations = %v, want no partial data", conversations)
	}
}

func TestApplyRejectsIncompleteSecondPageBeforeAnyLeave(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "missing channels", body: `{"ok":true}`},
		{name: "null channels", body: `{"ok":true,"channels":null}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			leaves := 0
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				switch request.URL.Path {
				case "/conversations.list":
					if formValue(t, request, "cursor") == "" {
						writeJSON(t, writer, `{"ok":true,"channels":[{"id":"C1","is_member":true}],"response_metadata":{"next_cursor":"second"}}`)
					} else {
						writeJSON(t, writer, test.body)
					}
				case "/conversations.info":
					writeJSON(t, writer, `{"ok":true,"channel":{"id":"C1","name":"old","is_general":false,"last_read":"1710000000.000000"}}`)
				case "/conversations.history":
					writeJSON(t, writer, `{"ok":true,"messages":[{"ts":"1710000000.000000"}]}`)
				case "/conversations.leave":
					leaves++
					writeJSON(t, writer, `{"ok":true}`)
				default:
					writer.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			result, err := usecase.Scan(context.Background(), newTestClient(t, server), usecase.Request{BeforeMonths: 1, Apply: true}, fixedClock{})
			if err == nil || !strings.Contains(err.Error(), "channels") {
				t.Errorf("Scan error = %v, want incomplete channels error", err)
			}
			if leaves != 0 || len(result.Decisions) != 0 {
				t.Errorf("leave count = %d, decisions = %v, want no leaves or partial decisions", leaves, result.Decisions)
			}
		})
	}
}

func TestApplyRejectsUnavailableHistoryBeforeAnyLeave(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "missing messages", status: http.StatusOK, body: `{"ok":true}`},
		{name: "null messages", status: http.StatusOK, body: `{"ok":true,"messages":null}`},
		{name: "Slack API error", status: http.StatusOK, body: `{"ok":false,"error":"missing_scope"}`},
		{name: "HTTP error", status: http.StatusTooManyRequests, body: `{"ok":false,"error":"ratelimited"}`},
		{name: "malformed JSON", status: http.StatusOK, body: `{`},
	} {
		t.Run(test.name, func(t *testing.T) {
			leaves := 0
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				switch request.URL.Path {
				case "/conversations.list":
					writeJSON(t, writer, `{"ok":true,"channels":[{"id":"C1","is_member":true},{"id":"C2","is_member":true}]}`)
				case "/conversations.info":
					id := formValue(t, request, "channel")
					writeJSON(t, writer, fmt.Sprintf(`{"ok":true,"channel":{"id":%q,"name":%q,"is_general":false,"last_read":"1710000000.000000"}}`, id, id))
				case "/conversations.history":
					if formValue(t, request, "channel") == "C1" {
						writeJSON(t, writer, `{"ok":true,"messages":[{"ts":"1710000000.000000"}]}`)
					} else {
						writer.Header().Set("Content-Type", "application/json")
						writer.WriteHeader(test.status)
						_, _ = io.WriteString(writer, test.body)
					}
				case "/conversations.leave":
					leaves++
					writeJSON(t, writer, `{"ok":true}`)
				default:
					writer.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			result, err := usecase.Scan(context.Background(), newTestClient(t, server), usecase.Request{BeforeMonths: 1, Apply: true}, fixedClock{})
			if err == nil || !strings.Contains(err.Error(), "history") || !strings.Contains(err.Error(), "C2") {
				t.Errorf("Scan error = %v, want history error for C2", err)
			}
			if leaves != 0 || len(result.Decisions) != 0 {
				t.Errorf("leave count = %d, decisions = %v, want no leaves or partial decisions", leaves, result.Decisions)
			}
		})
	}
}

func TestApplyTreatsEmptyHistoryAsUnknown(t *testing.T) {
	leaves := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/conversations.list":
			writeJSON(t, writer, `{"ok":true,"channels":[{"id":"C1","is_member":true}]}`)
		case "/conversations.info":
			writeJSON(t, writer, `{"ok":true,"channel":{"id":"C1","name":"empty","is_general":false,"last_read":"1710000000.000000"}}`)
		case "/conversations.history":
			writeJSON(t, writer, `{"ok":true,"messages":[]}`)
		case "/conversations.leave":
			leaves++
			writeJSON(t, writer, `{"ok":true}`)
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	result, err := usecase.Scan(context.Background(), newTestClient(t, server), usecase.Request{BeforeMonths: 1, Apply: true}, fixedClock{})
	if err != nil {
		t.Fatal(err)
	}
	if leaves != 0 {
		t.Errorf("leave count = %d, want 0", leaves)
	}
	if len(result.Decisions) != 1 || result.Decisions[0].Reason() != domain.ReasonUnknownActivity {
		t.Errorf("decisions = %v, want one unknown activity decision", result.Decisions)
	}
}

func TestListAcceptsEmptyChannelsArray(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writeJSON(t, writer, `{"ok":true,"channels":[]}`)
	}))
	defer server.Close()
	conversations, err := newTestClient(t, server).List(context.Background())
	if err != nil || len(conversations) != 0 {
		t.Fatalf("List() = %v, %v, want successful empty list", conversations, err)
	}
}

func TestLeaveUsesConversationsLeave(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got, want := request.URL.Path, "/conversations.leave"; got != want {
			t.Errorf("path = %q, want %q", got, want)
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		if err := request.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if got, want := request.Form.Get("channel"), "C1"; got != want {
			t.Errorf("channel = %q, want %q", got, want)
		}
		writeJSON(t, writer, `{"ok":true}`)
	}))
	defer server.Close()

	if err := newTestClient(t, server).Leave(context.Background(), testConversation(t, "C1")); err != nil {
		t.Fatal(err)
	}
}

func TestLeaveReportsSlackAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writeJSON(t, writer, `{"ok":false,"error":"cant_leave"}`)
	}))
	defer server.Close()

	if err := newTestClient(t, server).Leave(context.Background(), testConversation(t, "C1")); err == nil {
		t.Fatal("Leave() error = nil, want Slack API error")
	}
}

func TestLeaveTreatsNotInChannelAsSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writeJSON(t, writer, `{"ok":false,"error":"not_in_channel"}`)
	}))
	defer server.Close()

	if err := newTestClient(t, server).Leave(context.Background(), testConversation(t, "C1")); err != nil {
		t.Fatalf("Leave() error = %v, want nil", err)
	}
}

func newTestClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()

	client, err := New("test-token", server.Client(), server.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func testConversation(t *testing.T, id string) domain.Conversation {
	t.Helper()

	conversation, err := domain.NewConversation(domain.ServiceSlack, id, "test channel", domain.UnknownActivity(), domain.ProtectionNone)
	if err != nil {
		t.Fatal(err)
	}
	return conversation
}

func conversationIDs(conversations []domain.Conversation) []string {
	ids := make([]string, len(conversations))
	for index, conversation := range conversations {
		ids[index] = conversation.ID().Value()
	}
	return ids
}

func conversationNames(conversations []domain.Conversation) []string {
	names := make([]string, len(conversations))
	for index, conversation := range conversations {
		names[index] = conversation.Name()
	}
	return names
}

func writeJSON(t *testing.T, writer http.ResponseWriter, body string) {
	t.Helper()
	writer.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(writer).Encode(json.RawMessage(body)); err != nil {
		t.Fatal(err)
	}
}

func formValue(t *testing.T, request *http.Request, key string) string {
	t.Helper()
	if err := request.ParseForm(); err != nil {
		t.Fatal(err)
	}
	return request.Form.Get(key)
}
