package slack

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nlopes/slack"
)

func New(token string) *slack.Client {
	return slack.New(token)
}

func LastRead(channel *slack.Channel) time.Time {
	if channel != nil {
		lastRead, _ := toTime(channel.LastRead)
		return lastRead
	}
	return time.Now()
}

func StarredChannelIDs(client *slack.Client) []string {
	var stars []string
	//todo: equipment of the second return value for paging
	items, _, _ := client.GetStarred(slack.NewStarsParameters())
	for _, star := range items {
		switch star.Type {
		case slack.TYPE_CHANNEL:
			stars = append(stars, star.Channel)
		}
	}
	return stars
}

func MayBeLeaveChannel(client *slack.Client, channel slack.Channel, starredIDs []string) {
	if channel.IsMember {
		channelInfo, _ := client.GetChannelInfo(channel.ID)
		canLeave := canLeave(channelInfo, starredIDs)
		if canLeave {
			fmt.Println(channel.Name, LastRead(channelInfo), canLeave)
			client.LeaveChannel(channel.ID)
		}
	}
}

// condition of leaving channel
// 1. last of talking date is more than 1 month elapsed
// 2. however this shall not apply when its channel is added star
func canLeave(channel *slack.Channel, starredIDs []string) bool {
	lastRead := LastRead(channel)

	switch {
	case channel == nil:
		return false
	case includes(channel.ID, starredIDs):
		return false
	case lastRead.Before(time.Now().AddDate(0, -1, 0)):
		return true
	default:
		return false
	}
}

func includes(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

func toTime(lastReadUnix string) (time.Time, error) {
	unixStr := strings.Split(lastReadUnix, ".")[0]
	unix, error := strconv.ParseInt(unixStr, 10, 64)
	return time.Unix(unix, 0), error
}
