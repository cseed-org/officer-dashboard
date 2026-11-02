package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DirectMessenger sends a Discord direct message to one user.
type DirectMessenger interface {
	SendDM(ctx context.Context, discordUserID, content string) error
}

// DMError is a delivery failure with a message safe to show a director.
type DMError struct {
	Status int
	Code   int
	Msg    string
}

func (e *DMError) Error() string { return e.Msg }

// DiscordBot sends DMs through the Discord bot API (DISCORD_BOT_TOKEN). Opening a DM
// channel needs the bot and the recipient to share a server, and the recipient must
// allow DMs from server members.
type DiscordBot struct {
	Token   string
	BaseURL string // https://discord.com/api/v10 unless a test overrides it
	HTTP    *http.Client
}

// NewDiscordBot builds a bot client with a 5-second timeout per call.
func NewDiscordBot(token string) *DiscordBot {
	return &DiscordBot{Token: token, BaseURL: "https://discord.com/api/v10", HTTP: &http.Client{Timeout: 5 * time.Second}}
}

func (b *DiscordBot) call(ctx context.Context, path string, body any, out any) error {
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(b.BaseURL, "/")+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bot "+b.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "DiscordBot (https://cseed.co, 1.0) cseed-officer-dashboard")
	client := b.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Do(req)
	if err != nil {
		return &DMError{Msg: "Couldn't reach Discord. Try again, or copy the link instead."}
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	if res.StatusCode/100 != 2 {
		var e struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(data, &e)
		return dmFailure(res.StatusCode, e.Code)
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

// dmFailure maps Discord's error codes to plain advice.
func dmFailure(status, code int) *DMError {
	msg := fmt.Sprintf("Discord refused the DM (HTTP %d). Copy the link instead.", status)
	switch {
	case code == 50007:
		msg = "Discord won't deliver this DM: they don't share a server with the bot, or their DMs are closed. Copy the link instead."
	case code == 10013:
		msg = "Discord doesn't know that user ID. Check the ID, or copy the link instead."
	case status == http.StatusUnauthorized:
		msg = "The Discord bot token was rejected. Copy the link instead, and ask an owner to check DISCORD_BOT_TOKEN."
	case status == http.StatusTooManyRequests:
		msg = "Discord is rate-limiting the bot. Wait a minute, or copy the link instead."
	}
	return &DMError{Status: status, Code: code, Msg: msg}
}

func (b *DiscordBot) SendDM(ctx context.Context, discordUserID, content string) error {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var ch struct {
		ID string `json:"id"`
	}
	if err := b.call(ctx, "/users/@me/channels", map[string]string{"recipient_id": discordUserID}, &ch); err != nil {
		return err
	}
	if ch.ID == "" {
		return errors.New("discord returned no DM channel")
	}
	return b.call(ctx, "/channels/"+ch.ID+"/messages", map[string]any{
		"content":          trunc(content, 2000),
		"allowed_mentions": map[string]any{"parse": []string{}},
	}, nil)
}
