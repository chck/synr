package zulip

import (
	"context"
	"encoding/json"
	"errors"
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
	baseURL    *url.URL
	email      string
	apiKey     string
	httpClient *http.Client
}

type subscription struct {
	StreamID *int64  `json:"stream_id"`
	Name     *string `json:"name"`
	PinToTop *bool   `json:"pin_to_top"`
}

type message struct {
	Timestamp *int64 `json:"timestamp"`
}

type subscriptionsResponse struct {
	Result        *string         `json:"result"`
	Subscriptions *[]subscription `json:"subscriptions"`
}

type messagesResponse struct {
	Result   *string    `json:"result"`
	Messages *[]message `json:"messages"`
}

type unsubscribeResponse struct {
	Result     *string   `json:"result"`
	Removed    *[]string `json:"removed"`
	NotRemoved *[]string `json:"not_removed"`
}

func New(baseURL *url.URL, email string, apiKey string, httpClient *http.Client) (*Client, error) {
	if baseURL == nil {
		return nil, fmt.Errorf("Zulip base URL must not be nil")
	}
	if baseURL.Host == "" || (baseURL.Scheme != "http" && baseURL.Scheme != "https") {
		return nil, fmt.Errorf("Zulip base URL must be an absolute HTTP(S) URL")
	}
	if strings.TrimSpace(email) == "" {
		return nil, fmt.Errorf("Zulip email must not be blank")
	}
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("Zulip API key must not be blank")
	}
	if httpClient == nil {
		return nil, fmt.Errorf("Zulip HTTP client must not be nil")
	}

	baseURLCopy := *baseURL
	if !strings.HasSuffix(baseURLCopy.Path, "/") {
		baseURLCopy.Path += "/"
	}
	if baseURLCopy.RawPath != "" && !strings.HasSuffix(baseURLCopy.RawPath, "/") {
		baseURLCopy.RawPath += "/"
	}
	return &Client{baseURL: &baseURLCopy, email: email, apiKey: apiKey, httpClient: httpClient}, nil
}

func (client *Client) List(ctx context.Context) ([]domain.Conversation, error) {
	request, err := client.newRequest(ctx, http.MethodGet, "api/v1/users/me/subscriptions", nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create Zulip subscriptions request: %w", err)
	}

	var response subscriptionsResponse
	if err := client.doJSON(request, &response, "list Zulip subscriptions"); err != nil {
		return nil, err
	}
	if err := validateSuccess(response.Result, "list Zulip subscriptions"); err != nil {
		return nil, err
	}
	if response.Subscriptions == nil {
		return nil, fmt.Errorf("list Zulip subscriptions: response is missing subscriptions")
	}

	conversations := make([]domain.Conversation, 0, len(*response.Subscriptions))
	for _, subscription := range *response.Subscriptions {
		if err := subscription.validate(); err != nil {
			return nil, err
		}
		activity, err := client.newestActivity(ctx, *subscription.Name)
		if err != nil {
			return nil, err
		}

		protection := domain.ProtectionNone
		if *subscription.PinToTop {
			protection = domain.ProtectionPinned
		}
		conversation, err := domain.NewConversation(
			domain.ServiceZulip,
			strconv.FormatInt(*subscription.StreamID, 10),
			*subscription.Name,
			activity,
			protection,
		)
		if err != nil {
			return nil, fmt.Errorf("create Zulip subscription %q: %w", *subscription.Name, err)
		}
		conversations = append(conversations, conversation)
	}
	return conversations, nil
}

func (client *Client) Leave(ctx context.Context, conversation domain.Conversation) error {
	if conversation.Service() != domain.ServiceZulip {
		return fmt.Errorf("cannot leave non-Zulip conversation")
	}

	subscriptions, err := json.Marshal([]string{conversation.Name()})
	if err != nil {
		return fmt.Errorf("encode Zulip subscriptions: %w", err)
	}
	values := url.Values{"subscriptions": {string(subscriptions)}}
	request, err := client.newRequest(
		ctx,
		http.MethodDelete,
		"api/v1/users/me/subscriptions",
		nil,
		strings.NewReader(values.Encode()),
	)
	if err != nil {
		return fmt.Errorf("create Zulip unsubscribe request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	var response unsubscribeResponse
	if err := client.doJSON(request, &response, "leave Zulip subscription"); err != nil {
		return err
	}
	if err := validateSuccess(response.Result, "leave Zulip subscription"); err != nil {
		return err
	}
	if response.Removed == nil || response.NotRemoved == nil {
		return fmt.Errorf("leave Zulip subscription: response is missing removed or not_removed")
	}
	if contains(*response.Removed, conversation.Name()) {
		return nil
	}
	if contains(*response.NotRemoved, conversation.Name()) {
		return fmt.Errorf("leave Zulip subscription %q: not removed", conversation.Name())
	}
	return fmt.Errorf("leave Zulip subscription %q: response did not report the subscription", conversation.Name())
}

func (client *Client) newestActivity(ctx context.Context, channel string) (domain.Activity, error) {
	narrow, err := json.Marshal([]struct {
		Operator string `json:"operator"`
		Operand  string `json:"operand"`
	}{{Operator: "channel", Operand: channel}})
	if err != nil {
		return domain.Activity{}, fmt.Errorf("encode Zulip channel narrow: %w", err)
	}
	query := url.Values{
		"anchor":     {"newest"},
		"num_before": {"1"},
		"num_after":  {"0"},
		"narrow":     {string(narrow)},
	}
	request, err := client.newRequest(ctx, http.MethodGet, "api/v1/messages", query, nil)
	if err != nil {
		return domain.Activity{}, fmt.Errorf("create Zulip messages request: %w", err)
	}

	var response messagesResponse
	if err := client.doJSON(request, &response, "list Zulip messages"); err != nil {
		return domain.Activity{}, err
	}
	if err := validateSuccess(response.Result, "list Zulip messages"); err != nil {
		return domain.Activity{}, err
	}
	if response.Messages == nil {
		return domain.Activity{}, fmt.Errorf("list Zulip messages: response is missing messages")
	}
	if len(*response.Messages) == 0 {
		return domain.UnknownActivity(), nil
	}
	if (*response.Messages)[0].Timestamp == nil {
		return domain.Activity{}, fmt.Errorf("list Zulip messages: newest message is missing timestamp")
	}
	if *(*response.Messages)[0].Timestamp <= 0 {
		return domain.Activity{}, fmt.Errorf("list Zulip messages: newest message timestamp must be positive")
	}
	activity, err := domain.KnownActivity(time.Unix(*(*response.Messages)[0].Timestamp, 0))
	if err != nil {
		return domain.Activity{}, fmt.Errorf("create Zulip message activity: %w", err)
	}
	return activity, nil
}

func (client *Client) newRequest(ctx context.Context, method string, path string, query url.Values, body io.Reader) (*http.Request, error) {
	endpoint := client.endpoint(path)
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return nil, err
	}
	request.SetBasicAuth(client.email, client.apiKey)
	return request, nil
}

func (client *Client) endpoint(path string) *url.URL {
	return client.baseURL.ResolveReference(&url.URL{Path: path})
}

func (client *Client) doJSON(request *http.Request, target any, operation string) error {
	response, err := client.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return unexpectedStatus(operation, response)
	}
	if err := decodeJSON(response.Body, target); err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	return nil
}

func (subscription subscription) validate() error {
	if subscription.StreamID == nil {
		return fmt.Errorf("Zulip subscription is missing stream_id")
	}
	if *subscription.StreamID <= 0 {
		return fmt.Errorf("Zulip subscription stream_id must be positive")
	}
	if subscription.Name == nil || *subscription.Name == "" {
		return fmt.Errorf("Zulip subscription %d is missing name", *subscription.StreamID)
	}
	if subscription.PinToTop == nil {
		return fmt.Errorf("Zulip subscription %q is missing pin_to_top", *subscription.Name)
	}
	return nil
}

func validateSuccess(result *string, operation string) error {
	if result == nil {
		return fmt.Errorf("%s: response is missing result", operation)
	}
	if *result != "success" {
		return fmt.Errorf("%s: Zulip result is %q, want success", operation, *result)
	}
	return nil
}

func decodeJSON(body io.Reader, target any) error {
	decoder := json.NewDecoder(body)
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("decode JSON: unexpected additional JSON value")
		}
		return fmt.Errorf("decode trailing JSON data: %w", err)
	}
	return nil
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

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
