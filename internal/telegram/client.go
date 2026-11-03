package telegram

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
)

type Client struct {
	token  string
	client *http.Client
}

type ChatMember struct {
	Status             string `json:"status"`
	UntilDate          int64  `json:"until_date"`
	CanInviteUsers     bool   `json:"can_invite_users"`
	CanRestrictMembers bool   `json:"can_restrict_members"`
	User               struct {
		ID int64 `json:"id"`
	} `json:"user"`
}

func New(token string, client *http.Client) Client { return Client{token: token, client: client} }

func (t Client) GetChatMember(ctx context.Context, chatID, userID int64) (ChatMember, error) {
	var member ChatMember
	endpoint := "https://api.telegram.org/bot" + t.token + "/getChatMember?" + url.Values{
		"chat_id": {strconv.FormatInt(chatID, 10)},
		"user_id": {strconv.FormatInt(userID, 10)},
	}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return member, errors.New("cannot create Telegram request")
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return member, errors.New("telegram membership lookup failed")
	}
	defer func() { _ = resp.Body.Close() }()
	var result struct {
		OK     bool       `json:"ok"`
		Result ChatMember `json:"result"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&result) != nil || !result.OK {
		return member, fmt.Errorf("telegram membership lookup returned status %d", resp.StatusCode)
	}
	if result.Result.User.ID != userID {
		return member, errors.New("telegram returned a different user")
	}
	return result.Result, nil
}

func MemberUntil(unix int64) *time.Time {
	if unix <= 0 {
		return nil
	}
	value := time.Unix(unix, 0).UTC()
	return &value
}

func (t Client) Call(ctx context.Context, method string, params url.Values, output any) error {
	endpoint := "https://api.telegram.org/bot" + t.token + "/" + method
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(params.Encode()))
	if err != nil {
		return errors.New("cannot create Telegram request")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("telegram %s request failed", method)
	}
	defer func() { _ = resp.Body.Close() }()
	var envelope struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&envelope) != nil || !envelope.OK {
		return fmt.Errorf("telegram %s returned status %d", method, resp.StatusCode)
	}
	if err := json.Unmarshal(envelope.Result, output); err != nil {
		return fmt.Errorf("telegram %s returned invalid data", method)
	}
	return nil
}
