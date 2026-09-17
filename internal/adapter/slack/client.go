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

		for _, channel := range channels {
			if !channel.IsMember {
				continue
			}

			info, err := client.api.GetConversationInfoContext(ctx, &slackapi.GetConversationInfoInput{ChannelID: channel.ID})
			if err != nil {
				return nil, fmt.Errorf("get Slack conversation %q info: %w", channel.ID, err)
			}

			conversation, err := conversationFromChannel(*info)
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

func conversationFromChannel(channel slackapi.Channel) (domain.Conversation, error) {
	activity := activityFromLastRead(channel.LastRead)
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

func activityFromLastRead(lastRead string) domain.Activity {
	if !slackTimestampPattern.MatchString(lastRead) {
		return domain.UnknownActivity()
	}

	seconds, err := strconv.ParseInt(strings.SplitN(lastRead, ".", 2)[0], 10, 64)
	if err != nil || seconds <= 0 {
		return domain.UnknownActivity()
	}

	activity, err := domain.KnownActivity(time.Unix(seconds, 0))
	if err != nil {
		return domain.UnknownActivity()
	}
	return activity
}
