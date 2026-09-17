package slack

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	slackapi "github.com/slack-go/slack"

	"github.com/chck/synr/internal/domain"
)

type Client struct {
	api *slackapi.Client
}

var slackTimestampPattern = regexp.MustCompile(`^[0-9]+\.[0-9]{6}$`)

func New(token string, httpClient *http.Client, apiURL string) (*Client, error) {
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("Slack token must not be blank")
	}
	if httpClient == nil {
		return nil, fmt.Errorf("Slack HTTP client must not be nil")
	}
	if strings.TrimSpace(apiURL) == "" {
		return nil, fmt.Errorf("Slack API URL must not be blank")
	}

	return &Client{api: slackapi.New(token, slackapi.OptionHTTPClient(httpClient), slackapi.OptionAPIURL(apiURL))}, nil
}

func (client *Client) List(ctx context.Context) ([]domain.Conversation, error) {
	var conversations []domain.Conversation
	var cursor string
	for {
		channels, nextCursor, err := client.api.GetConversationsContext(ctx, &slackapi.GetConversationsParameters{
			Cursor:          cursor,
			ExcludeArchived: true,
			Limit:           200,
			Types:           []string{"public_channel", "private_channel"},
		})
		if err != nil {
			return nil, fmt.Errorf("list Slack conversations: %w", err)
		}
		if channels == nil {
			return nil, fmt.Errorf("list Slack conversations: response is missing a channels array")
		}

		for _, channel := range channels {
			if !channel.IsMember {
				continue
			}

			info, err := client.api.GetConversationInfoContext(ctx, &slackapi.GetConversationInfoInput{ChannelID: channel.ID})
			if err != nil {
				return nil, fmt.Errorf("get Slack conversation %q info: %w", channel.ID, err)
			}

			activity, err := client.latestActivity(ctx, channel.ID)
			if err != nil {
				return nil, fmt.Errorf("get Slack conversation %q history: %w", channel.ID, err)
			}

			conversation, err := conversationFromChannel(*info, activity)
			if err != nil {
				return nil, err
			}
			conversations = append(conversations, conversation)
		}

		if nextCursor == "" {
			return conversations, nil
		}
		cursor = nextCursor
	}
}

func (client *Client) latestActivity(ctx context.Context, channelID string) (domain.Activity, error) {
	activity := domain.UnknownActivity()
	var cursor string
	seenCursors := make(map[string]struct{})
	for {
		history, err := client.api.GetConversationHistoryContext(ctx, &slackapi.GetConversationHistoryParameters{
			ChannelID: channelID,
			Cursor:    cursor,
			Limit:     200,
		})
		if err != nil {
			return domain.Activity{}, err
		}
		if history.Messages == nil {
			return domain.Activity{}, fmt.Errorf("response is missing a messages array")
		}
		for _, message := range history.Messages {
			candidate, err := activityFromMessage(message)
			if err != nil {
				return domain.Activity{}, err
			}
			if !activity.Known() || candidate.At().After(activity.At()) {
				activity = candidate
			}
		}
		cursor = history.ResponseMetaData.NextCursor
		if cursor == "" {
			if history.HasMore {
				return domain.Activity{}, fmt.Errorf("incomplete history: has_more is true without a next cursor")
			}
			return activity, nil
		}
		if _, seen := seenCursors[cursor]; seen {
			return domain.Activity{}, fmt.Errorf("incomplete history: repeated pagination cursor")
		}
		seenCursors[cursor] = struct{}{}
	}
}

func (client *Client) Leave(ctx context.Context, conversation domain.Conversation) error {
	if conversation.Service() != domain.ServiceSlack {
		return fmt.Errorf("cannot leave non-Slack conversation")
	}

	_, err := client.api.LeaveConversationContext(ctx, conversation.ID().Value())
	if err == nil {
		return nil
	}

	var slackError slackapi.SlackErrorResponse
	if errors.As(err, &slackError) && slackError.Err == "not_in_channel" {
		return nil
	}
	return fmt.Errorf("leave Slack conversation %q: %w", conversation.ID().Value(), err)
}

func conversationFromChannel(channel slackapi.Channel, activity domain.Activity) (domain.Conversation, error) {
	protection := domain.ProtectionNone
	if channel.IsGeneral {
		protection = domain.ProtectionGeneral
	}

	conversation, err := domain.NewConversation(domain.ServiceSlack, channel.ID, channel.Name, activity, protection)
	if err != nil {
		return domain.Conversation{}, fmt.Errorf("create Slack conversation %q: %w", channel.ID, err)
	}
	return conversation, nil
}

func activityFromMessage(message slackapi.Message) (domain.Activity, error) {
	activity, err := activityFromTimestamp(message.Timestamp)
	if err != nil {
		return domain.Activity{}, fmt.Errorf("message ts: %w", err)
	}
	if message.ReplyCount < 0 {
		return domain.Activity{}, fmt.Errorf("message reply_count must not be negative")
	}
	if message.LatestReply == "" {
		if message.ReplyCount > 0 {
			return domain.Activity{}, fmt.Errorf("message with replies is missing latest_reply")
		}
		return activity, nil
	}
	latestReply, err := activityFromTimestamp(message.LatestReply)
	if err != nil {
		return domain.Activity{}, fmt.Errorf("message latest_reply: %w", err)
	}
	if latestReply.At().After(activity.At()) {
		return latestReply, nil
	}
	return activity, nil
}

func activityFromTimestamp(timestamp string) (domain.Activity, error) {
	if !slackTimestampPattern.MatchString(timestamp) {
		return domain.Activity{}, fmt.Errorf("invalid timestamp %q", timestamp)
	}

	parts := strings.SplitN(timestamp, ".", 2)
	seconds, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return domain.Activity{}, fmt.Errorf("invalid timestamp seconds: %w", err)
	}
	if seconds <= 0 {
		return domain.Activity{}, fmt.Errorf("timestamp seconds must be positive")
	}
	microseconds, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return domain.Activity{}, fmt.Errorf("invalid timestamp fraction: %w", err)
	}

	return domain.KnownActivity(time.Unix(seconds, microseconds*1000))
}
