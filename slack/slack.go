package slack

import (
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/nlopes/slack"
)

func New(token string) *slack.Client {
	return slack.New(token)
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

func MayBeLeaveChannel(canDryRun bool, beforeMonth int, client *slack.Client, channel slack.Channel, starredIDs []string) {
	if channel.IsMember {
		channelInfo, _ := client.GetChannelInfo(channel.ID)
		canLeave := canLeave(channelInfo, starredIDs, beforeMonth)
		if canLeave {
			log.Println(channel.Name, lastRead(channelInfo), canLeave)
			if !canDryRun {
				client.LeaveChannel(channel.ID)
			}
		}
	}
}

// condition of leaving channel
// 1. last of talking date is more than X month elapsed
// 2. however this shall not apply when its channel is added star
func canLeave(channel *slack.Channel, starredIDs []string, beforeMonth int) bool {

	switch {
	case channel == nil:
		return false
	case includes(channel.ID, starredIDs):
		return false
	case beforeLastRead(channel, beforeMonth):
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

func beforeLastRead(channel *slack.Channel, beforeMonth int) bool {
	if 0 >= beforeMonth {
		beforeMonth = 1
	}
	return lastRead(channel).Before(time.Now().AddDate(0, beforeMonth*-1, 0))
}

func lastRead(channel *slack.Channel) time.Time {
	if channel != nil {
		lastRead, _ := toTime(channel.LastRead)
		return lastRead
	}
	return time.Now()
}
