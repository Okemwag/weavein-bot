package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
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

// StatusError preserves a Bot API HTTP status without exposing provider details
// or credentials to callers.
type StatusError struct {
	Method      string
	Code        int
	Description string
	RetryAfter  int
}

// RetryDelay is how long Telegram asked callers to wait, with a safe minimum.
func (e StatusError) RetryDelay() time.Duration {
	if e.RetryAfter > 0 {
		return time.Duration(e.RetryAfter) * time.Second
	}
	return 30 * time.Second
}

// RequestError is a failed request with no Telegram response. Unsent means the
// connection was never made, so Telegram cannot have acted and a retry is safe.
type RequestError struct {
	Method string
	Unsent bool
}

func (e RequestError) Error() string { return fmt.Sprintf("telegram %s request failed", e.Method) }

func requestError(method string, err error) RequestError {
	var op *net.OpError
	var dns *net.DNSError
	unsent := errors.As(err, &dns) || (errors.As(err, &op) && op.Op == "dial")
	return RequestError{Method: method, Unsent: unsent}
}

func (e StatusError) Error() string {
	return fmt.Sprintf("telegram %s returned status %d", e.Method, e.Code)
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
		return member, requestError("getChatMember", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var result struct {
		OK     bool       `json:"ok"`
		Result ChatMember `json:"result"`
	}
	if resp.StatusCode != http.StatusOK {
		return member, responseStatusError(resp, "getChatMember")
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&result) != nil || !result.OK {
		return member, errors.New("telegram membership lookup returned invalid data")
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
		return requestError(method, err)
	}
	defer func() { _ = resp.Body.Close() }()
	var envelope struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
	}
	if resp.StatusCode != http.StatusOK {
		return responseStatusError(resp, method)
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&envelope) != nil || !envelope.OK {
		return fmt.Errorf("telegram %s returned invalid data", method)
	}
	if err := json.Unmarshal(envelope.Result, output); err != nil {
		return fmt.Errorf("telegram %s returned invalid data", method)
	}
	return nil
}

func responseStatusError(resp *http.Response, method string) StatusError {
	var body struct {
		Description string `json:"description"`
		Parameters  struct {
			RetryAfter int `json:"retry_after"`
		} `json:"parameters"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&body)
	return StatusError{Method: method, Code: resp.StatusCode, Description: body.Description, RetryAfter: body.Parameters.RetryAfter}
}
