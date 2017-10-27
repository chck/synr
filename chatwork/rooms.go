package chatwork

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

type Room struct {
	RoomID         int      `json:"room_id"`
	Name           string   `json:"name"`
	Type           string   `json:"type"`
	Role           string   `json:"role"`
	Sticky         bool     `json:"sticky"`
	UnreadNum      int      `json:"unread_num"`
	MentionNum     int      `json:"mention_num"`
	MytaskNum      int      `json:"mytask_num"`
	MessageNum     int      `json:"message_num"`
	FileNum        int      `json:"file_num"`
	TaskNum        int      `json:"task_num"`
	IconPath       string   `json:"icon_path"`
	LastUpdateTime JSONTime `json:"last_update_time"`
}

type RoomRequest struct {
	Description *string `json:"description,omitempty"`
}

func (api *Client) GetRooms() ([]Room, error) {
	endpoint := defaultBaseURL + "rooms"

	req, err := http.NewRequest("GET", endpoint, nil)
	req.Header.Set("X-ChatWorkToken", api.config.token)
	if err != nil {
		return nil, err
	}

	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	var rooms []Room
	err = json.NewDecoder(res.Body).Decode(&rooms)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	return rooms, nil
}

func (api *Client) LeaveRooms(roomID string) (bool, error) {
	endpoint := defaultBaseURL + "rooms/" + roomID
	values := url.Values{
		"action_type": {"leave"},
	}
	req, err := http.NewRequest("DELETE", endpoint, strings.NewReader(values.Encode()))
	req.Header.Set("X-ChatWorkToken", api.config.token)
	if err != nil {
		return false, err
	}

	client := &http.Client{}
	_, err = client.Do(req)
	if err != nil {
		return false, err
	}

	return true, nil
}
