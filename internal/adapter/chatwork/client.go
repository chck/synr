package chatwork

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/chck/synr/internal/domain"
)

const diagnosticBodyLimit int64 = 4096

type Client struct {
	token      string
	baseURL    *url.URL
	httpClient *http.Client
}

type room struct {
	ID             int64  `json:"room_id"`
	Name           string `json:"name"`
	Type           string `json:"type"`
	Sticky         bool   `json:"sticky"`
	LastUpdateTime int64  `json:"last_update_time"`
}

func New(token string, baseURL *url.URL, httpClient *http.Client) (*Client, error) {
	if token == "" {
		return nil, fmt.Errorf("Chatwork token must not be empty")
	}
	if baseURL == nil {
		return nil, fmt.Errorf("Chatwork base URL must not be nil")
	}
	if baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, fmt.Errorf("Chatwork base URL must be absolute")
	}
	if httpClient == nil {
		return nil, fmt.Errorf("Chatwork HTTP client must not be nil")
	}

	baseURLCopy := *baseURL
	if !strings.HasSuffix(baseURLCopy.Path, "/") {
		baseURLCopy.Path += "/"
	}
	if baseURLCopy.RawPath != "" && !strings.HasSuffix(baseURLCopy.RawPath, "/") {
		baseURLCopy.RawPath += "/"
	}
	return &Client{token: token, baseURL: &baseURLCopy, httpClient: httpClient}, nil
}

func (client *Client) List(ctx context.Context) ([]domain.Conversation, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, client.endpoint("rooms").String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create Chatwork rooms request: %w", err)
	}
	request.Header.Set("X-ChatWorkToken", client.token)

	response, err := client.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request Chatwork rooms: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, unexpectedStatus("list Chatwork rooms", response)
	}

	var rooms []room
	if err := json.NewDecoder(response.Body).Decode(&rooms); err != nil {
		return nil, fmt.Errorf("decode Chatwork rooms: %w", err)
	}

	conversations := make([]domain.Conversation, 0, len(rooms))
	for _, room := range rooms {
		conversation, err := room.conversation()
		if err != nil {
			return nil, err
		}
		conversations = append(conversations, conversation)
	}
	return conversations, nil
}

func (client *Client) Leave(ctx context.Context, conversation domain.Conversation) error {
	if conversation.Service() != domain.ServiceChatwork {
		return fmt.Errorf("cannot leave non-Chatwork conversation")
	}

	values := url.Values{"action_type": {"leave"}}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodDelete,
		client.roomEndpoint(conversation.ID().Value()).String(),
		strings.NewReader(values.Encode()),
	)
	if err != nil {
		return fmt.Errorf("create Chatwork leave request: %w", err)
	}
	request.Header.Set("X-ChatWorkToken", client.token)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := client.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("request Chatwork leave: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusNoContent {
		return unexpectedStatus("leave Chatwork room", response)
	}
	return nil
}

func (client *Client) endpoint(path string) *url.URL {
	return client.baseURL.ResolveReference(&url.URL{Path: path})
}

func (client *Client) roomEndpoint(id string) *url.URL {
	endpoint := client.endpoint("rooms/")
	escapedPath := endpoint.EscapedPath()
	escapedID := url.PathEscape(id)
	endpoint.Path += id
	endpoint.RawPath = escapedPath + escapedID
	return endpoint
}

func (room room) conversation() (domain.Conversation, error) {
	activity := domain.UnknownActivity()
	if room.LastUpdateTime != 0 {
		var err error
		activity, err = domain.KnownActivity(time.Unix(room.LastUpdateTime, 0))
		if err != nil {
			return domain.Conversation{}, fmt.Errorf("create Chatwork room %d activity: %w", room.ID, err)
		}
	}

	protection := domain.ProtectionNone
	switch {
	case room.Type == "direct":
		protection = domain.ProtectionDirect
	case room.Sticky:
		protection = domain.ProtectionSticky
	}

	conversation, err := domain.NewConversation(
		domain.ServiceChatwork,
		strconv.FormatInt(room.ID, 10),
		room.Name,
		activity,
		protection,
	)
	if err != nil {
		return domain.Conversation{}, fmt.Errorf("create Chatwork room %d: %w", room.ID, err)
	}
	return conversation, nil
}

func unexpectedStatus(operation string, response *http.Response) error {
	body, err := io.ReadAll(io.LimitReader(response.Body, diagnosticBodyLimit))
	if err != nil {
		return fmt.Errorf("%s: unexpected HTTP status %d (read response body: %w)", operation, response.StatusCode, err)
	}
	if diagnostic := strings.TrimSpace(string(body)); diagnostic != "" {
		return fmt.Errorf("%s: unexpected HTTP status %d: %s", operation, response.StatusCode, diagnostic)
	}
	return fmt.Errorf("%s: unexpected HTTP status %d", operation, response.StatusCode)
}
