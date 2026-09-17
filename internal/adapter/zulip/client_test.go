package zulip

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestListFetchesNewestMessageForEverySubscription(t *testing.T) {
	var requestedChannels []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/users/me/subscriptions":
			if request.Method != http.MethodGet {
				t.Errorf("subscriptions method = %s, want GET", request.Method)
			}
			writeJSON(t, writer, `{"result":"success","subscriptions":[{"stream_id":1,"name":"Denmark","pin_to_top":false},{"stream_id":2,"name":"Norway","pin_to_top":false}]}`)
		case "/api/v1/messages":
			channel := requestedChannel(t, request)
			requestedChannels = append(requestedChannels, channel)
			timestamps := map[string]int64{"Denmark": 1710000000, "Norway": 1710000001}
			writeJSON(t, writer, fmt.Sprintf(`{"result":"success","messages":[{"timestamp":%d}]}`, timestamps[channel]))
		default:
			t.Errorf("path = %s, want Zulip endpoint", request.URL.Path)
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	conversations, err := newTestClient(t, server).List(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if got, want := requestedChannels, []string{"Denmark", "Norway"}; !reflect.DeepEqual(got, want) {
		t.Errorf("message channels = %v, want %v", got, want)
	}
	if got, want := conversationIDs(conversations), []string{"1", "2"}; !reflect.DeepEqual(got, want) {
		t.Errorf("conversation IDs = %v, want %v", got, want)
	}
	if got, want := conversationNames(conversations), []string{"Denmark", "Norway"}; !reflect.DeepEqual(got, want) {
		t.Errorf("conversation names = %v, want %v", got, want)
	}
	for index, timestamp := range []int64{1710000000, 1710000001} {
		activity := conversations[index].Activity()
		if !activity.Known() || !activity.At().Equal(time.Unix(timestamp, 0)) {
			t.Errorf("conversation %d activity = (%v, %v), want known %v", index, activity.Known(), activity.At(), time.Unix(timestamp, 0))
		}
	}
}

func TestListProtectsPinnedSubscription(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/users/me/subscriptions":
			writeJSON(t, writer, `{"result":"success","subscriptions":[{"stream_id":42,"name":"Pinned","pin_to_top":true}]}`)
		case "/api/v1/messages":
			requestedChannel(t, request)
			writeJSON(t, writer, `{"result":"success","messages":[{"timestamp":1710000000}]}`)
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	conversations, err := newTestClient(t, server).List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(conversations) != 1 {
		t.Fatalf("conversation count = %d, want 1", len(conversations))
	}
	if got := conversations[0].Protection(); got != domain.ProtectionPinned {
		t.Errorf("protection = %q, want %q", got, domain.ProtectionPinned)
	}
}

func TestListTreatsChannelWithoutMessagesAsUnknown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/users/me/subscriptions":
			writeJSON(t, writer, `{"result":"success","subscriptions":[{"stream_id":7,"name":"Empty","pin_to_top":false}]}`)
		case "/api/v1/messages":
			requestedChannel(t, request)
			writeJSON(t, writer, `{"result":"success","messages":[]}`)
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
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

func TestListFailsIfAnyActivityRequestFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/users/me/subscriptions":
			writeJSON(t, writer, `{"result":"success","subscriptions":[{"stream_id":1,"name":"First","pin_to_top":false},{"stream_id":2,"name":"Second","pin_to_top":false}]}`)
		case "/api/v1/messages":
			if requestedChannel(t, request) == "First" {
				writeJSON(t, writer, `{"result":"success","messages":[{"timestamp":1710000000}]}`)
				return
			}
			http.Error(writer, "unavailable", http.StatusServiceUnavailable)
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	conversations, err := newTestClient(t, server).List(context.Background())
	if err == nil {
		t.Fatal("List() error = nil, want activity request error")
	}
	if conversations != nil {
		t.Errorf("List() conversations = %v, want no partial data", conversations)
	}
}

func TestListFailsClosedForIncompleteActivityResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/users/me/subscriptions":
			writeJSON(t, writer, `{"result":"success","subscriptions":[{"stream_id":1,"name":"Missing messages","pin_to_top":false}]}`)
		case "/api/v1/messages":
			requestedChannel(t, request)
			writeJSON(t, writer, `{"result":"success"}`)
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	conversations, err := newTestClient(t, server).List(context.Background())
	if err == nil {
		t.Fatal("List() error = nil, want incomplete activity response error")
	}
	if conversations != nil {
		t.Errorf("List() conversations = %v, want no partial data", conversations)
	}
}

func TestListRejectsNonPositiveStreamID(t *testing.T) {
	for _, streamID := range []int64{0, -1} {
		t.Run(fmt.Sprintf("stream ID %d", streamID), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				switch request.URL.Path {
				case "/api/v1/users/me/subscriptions":
					writeJSON(t, writer, fmt.Sprintf(`{"result":"success","subscriptions":[{"stream_id":%d,"name":"Invalid","pin_to_top":false}]}`, streamID))
				case "/api/v1/messages":
					requestedChannel(t, request)
					writeJSON(t, writer, `{"result":"success","messages":[{"timestamp":1710000000}]}`)
				default:
					writer.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()

			conversations, err := newTestClient(t, server).List(context.Background())
			if err == nil {
				t.Fatal("List() error = nil, want invalid stream ID error")
			}
			if conversations != nil {
				t.Errorf("List() conversations = %v, want no partial data", conversations)
			}
		})
	}
}

func TestLeaveSendsEncodedChannelName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodDelete {
			t.Errorf("method = %s, want DELETE", request.Method)
		}
		if request.URL.Path != "/api/v1/users/me/subscriptions" {
			t.Errorf("path = %s, want /api/v1/users/me/subscriptions", request.URL.Path)
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
		if got := form.Get("subscriptions"); got != `["Denmark"]` {
			t.Errorf("subscriptions = %q, want [\\\"Denmark\\\"]", got)
		}
		writeJSON(t, writer, `{"result":"success","removed":["Denmark"],"not_removed":[]}`)
	}))
	defer server.Close()

	conversation := testConversation(t, "12", "Denmark")
	if err := newTestClient(t, server).Leave(context.Background(), conversation); err != nil {
		t.Fatal(err)
	}
}

func TestRequestsUseBasicAuthentication(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		email, apiKey, ok := request.BasicAuth()
		if !ok || email != "test@example.com" || apiKey != "test-api-key" {
			t.Errorf("BasicAuth() = (%q, %q, %v), want test credentials", email, apiKey, ok)
		}
		switch request.URL.Path {
		case "/api/v1/users/me/subscriptions":
			writeJSON(t, writer, `{"result":"success","subscriptions":[{"stream_id":1,"name":"Authenticated","pin_to_top":false}]}`)
		case "/api/v1/messages":
			requestedChannel(t, request)
			writeJSON(t, writer, `{"result":"success","messages":[]}`)
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	if _, err := newTestClient(t, server).List(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func newTestClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	baseURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client, err := New(baseURL, "test@example.com", "test-api-key", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func requestedChannel(t *testing.T, request *http.Request) string {
	t.Helper()
	if request.Method != http.MethodGet {
		t.Errorf("messages method = %s, want GET", request.Method)
	}
	query := request.URL.Query()
	if got := query.Get("anchor"); got != "newest" {
		t.Errorf("anchor = %q, want newest", got)
	}
	if got := query.Get("num_before"); got != "1" {
		t.Errorf("num_before = %q, want 1", got)
	}
	if got := query.Get("num_after"); got != "0" {
		t.Errorf("num_after = %q, want 0", got)
	}
	var narrow []struct {
		Operator string `json:"operator"`
		Operand  string `json:"operand"`
	}
	if err := json.Unmarshal([]byte(query.Get("narrow")), &narrow); err != nil {
		t.Fatalf("decode narrow = %v", err)
	}
	if len(narrow) != 1 || narrow[0].Operator != "channel" || narrow[0].Operand == "" {
		t.Fatalf("narrow = %v, want a channel predicate", narrow)
	}
	return narrow[0].Operand
}

func writeJSON(t *testing.T, writer http.ResponseWriter, body string) {
	t.Helper()
	writer.Header().Set("Content-Type", "application/json")
	if _, err := io.WriteString(writer, body); err != nil {
		t.Fatal(err)
	}
}

func testConversation(t *testing.T, id string, name string) domain.Conversation {
	t.Helper()
	conversation, err := domain.NewConversation(domain.ServiceZulip, id, name, domain.UnknownActivity(), domain.ProtectionNone)
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

func conversationNames(conversations []domain.Conversation) []string {
	names := make([]string, 0, len(conversations))
	for _, conversation := range conversations {
		names = append(names, conversation.Name())
	}
	return names
}

func TestNewRejectsInvalidConfiguration(t *testing.T) {
	validURL, err := url.Parse("https://zulip.example.com")
	if err != nil {
		t.Fatal(err)
	}
	relativeURL, err := url.Parse("/api")
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name       string
		baseURL    *url.URL
		email      string
		apiKey     string
		httpClient *http.Client
	}{
		{name: "nil URL", email: "a@example.com", apiKey: "key", httpClient: http.DefaultClient},
		{name: "relative URL", baseURL: relativeURL, email: "a@example.com", apiKey: "key", httpClient: http.DefaultClient},
		{name: "unsupported scheme", baseURL: &url.URL{Scheme: "ftp", Host: "zulip.example.com"}, email: "a@example.com", apiKey: "key", httpClient: http.DefaultClient},
		{name: "blank email", baseURL: validURL, apiKey: "key", httpClient: http.DefaultClient},
		{name: "blank API key", baseURL: validURL, email: "a@example.com", httpClient: http.DefaultClient},
		{name: "nil HTTP client", baseURL: validURL, email: "a@example.com", apiKey: "key"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := New(test.baseURL, test.email, test.apiKey, test.httpClient); err == nil {
				t.Fatal("New() error = nil, want configuration error")
			}
		})
	}
}

func TestLeaveReportsNotRemovedSubscription(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writeJSON(t, writer, `{"result":"success","removed":[],"not_removed":["Denmark"]}`)
	}))
	defer server.Close()

	err := newTestClient(t, server).Leave(context.Background(), testConversation(t, "12", "Denmark"))
	if err == nil || !strings.Contains(err.Error(), "not removed") {
		t.Fatalf("Leave() error = %v, want not removed error", err)
	}
}
