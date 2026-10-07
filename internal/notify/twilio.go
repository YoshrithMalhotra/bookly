package notify

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
)

// TwilioSender sends WhatsApp messages through Twilio's Messages API.
//
// WhatsApp only allows free-form text inside a 24h window after the customer
// last wrote to you. Reminders and review requests are business-initiated,
// so in production they need approved templates: set ContentSIDs to the
// Content SID of each approved template (keyed by Message.Kind).
type TwilioSender struct {
	AccountSID  string
	AuthToken   string
	From        string            // WhatsApp-enabled number, E.164
	ContentSIDs map[string]string // kind -> Content SID
	BaseURL     string            // defaults to https://api.twilio.com
	Client      *http.Client
}

func (t *TwilioSender) Send(ctx context.Context, msg Message) error {
	form := url.Values{
		"To":   {"whatsapp:" + msg.To},
		"From": {"whatsapp:" + t.From},
	}
	if sid := t.ContentSIDs[msg.Kind]; sid != "" {
		vars := make(map[string]string, len(msg.Vars))
		for i, v := range msg.Vars {
			vars[strconv.Itoa(i+1)] = v
		}
		b, _ := json.Marshal(vars)
		form.Set("ContentSid", sid)
		form.Set("ContentVariables", string(b))
	} else {
		form.Set("Body", msg.Body)
	}

	base := t.BaseURL
	if base == "" {
		base = "https://api.twilio.com"
	}
	endpoint := fmt.Sprintf("%s/2010-04-01/Accounts/%s/Messages.json", base, url.PathEscape(t.AccountSID))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.SetBasicAuth(t.AccountSID, t.AuthToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := t.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("twilio: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	var apiErr struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &apiErr)
	err = fmt.Errorf("twilio: HTTP %d: code %d: %s", resp.StatusCode, apiErr.Code, apiErr.Message)
	// 429 and 5xx are worth retrying; other 4xx (bad number, bad template,
	// bad credentials) will fail the same way every time.
	if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
		return &PermanentError{Err: err}
	}
	return err
}
