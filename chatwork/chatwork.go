package chatwork

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	defaultBaseURL = "https://api.chatwork.com/v2/"
)

type Client struct {
	config struct {
		token string
	}

	BaseURL *url.URL
}

func New(token string) *Client {
	baseURL, _ := url.Parse(defaultBaseURL)

	c := &Client{BaseURL: baseURL}
	c.config.token = token
	return c
}

func (c *Client) NewRequest(method, urlStr string, body interface{}) (*http.Request, error) {
	rel, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}

	u := c.BaseURL.ResolveReference(rel)

	var buf io.ReadWriter
	if body != nil {
		buf = new(bytes.Buffer)
		err := json.NewEncoder(buf).Encode(body)
		if err != nil {
			return nil, err
		}
	}

	req, err := http.NewRequest(method, u.String(), buf)
	if err != nil {
		return nil, err
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return req, nil
}

func MayBeLeaveRoom(canDryRun bool, beforeMonth int, client *Client, room *Room) {
	canLeave := canLeave(room, beforeMonth)
	if canLeave {
		log.Println(room.Name, room.LastUpdateTime, canLeave, room.Type)
		if !canDryRun {
			client.LeaveRooms(strconv.Itoa(room.RoomID))
		}
	}
}

func canLeave(room *Room, beforeMonth int) bool {
	switch {
	case room == nil:
		return false
	case room.Sticky:
		return false
	case room.Type == "direct":
		return false
	case beforeLastRead(room, beforeMonth):
		return true
	default:
		return false
	}
}

func rawLastRead(lastUpdateTime JSONTime) string {
	return fmt.Sprintf("%d", lastUpdateTime)
}

func beforeLastRead(room *Room, beforeMonth int) bool {
	if 0 >= beforeMonth {
		beforeMonth = 1
	}
	return lastRead(room).Before(time.Now().AddDate(0, beforeMonth*-1, 0))
}

func lastRead(room *Room) time.Time {
	if room != nil {
		return room.LastUpdateTime.Time()
	}
	return time.Now()
}
