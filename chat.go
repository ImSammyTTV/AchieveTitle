package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// eventsubURL is Twitch's EventSub WebSocket, used to read chat messages.
var eventsubURL = "wss://eventsub.wss.twitch.tv/ws"

const maxChatLen = 500 // Twitch's chat message limit

// Chat answers chat commands and announces unlocks, as the streamer's bot
// account or (as a fallback) their own channel account.
type Chat struct {
	store  *Store
	worker *Worker
	twitch *Twitch
	reload chan struct{}

	mu       sync.Mutex
	status   string
	acc      chatAccount // account currently connected, zero when not connected
	lastUsed map[string]time.Time
}

type chatAccount struct {
	token, userID, login string
}

func newChat(store *Store, worker *Worker) *Chat {
	return &Chat{store: store, worker: worker, twitch: worker.twitch, reload: make(chan struct{}, 1),
		status: "off", lastUsed: map[string]time.Time{}}
}

// Reload makes the chat connection pick up changed settings or logins.
func (c *Chat) Reload() {
	select {
	case c.reload <- struct{}{}:
	default:
	}
}

func (c *Chat) Status() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

func (c *Chat) setStatus(s string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if s != c.status && strings.HasPrefix(s, "error") {
		log.Println("chat:", s)
	}
	c.status = s
}

// account picks who sends replies, following the "reply as" setting.
func account(cfg Config) (chatAccount, error) {
	if cfg.TwitchToken == "" {
		return chatAccount{}, errors.New("connect your Twitch channel first")
	}
	if cfg.Chat.ReplyAs == "channel" {
		if !hasScopes(cfg.TwitchScopes, botScope) {
			return chatAccount{}, errors.New("reconnect your Twitch channel to allow chat replies from it")
		}
		return chatAccount{cfg.TwitchToken, cfg.TwitchUserID, cfg.TwitchLogin}, nil
	}
	if cfg.BotToken == "" {
		return chatAccount{}, errors.New("connect a bot account, or choose to reply from your own channel")
	}
	return chatAccount{cfg.BotToken, cfg.BotUserID, cfg.BotLogin}, nil
}

// Run keeps a chat connection open while chat commands are switched on.
func (c *Chat) Run(ctx context.Context) {
	backoff := 5 * time.Second
	for ctx.Err() == nil {
		cfg := c.store.Get()
		if !cfg.Chat.Enabled {
			c.setStatus("off")
			c.wait(ctx, 0)
			continue
		}
		acc, err := account(cfg)
		if err != nil {
			c.setStatus("needs setup: " + err.Error())
			c.wait(ctx, 0)
			continue
		}

		c.setStatus("connecting as " + acc.login)
		sctx, cancel := context.WithCancel(ctx)
		go func() { // settings changed: reconnect with the new ones
			select {
			case <-c.reload:
				cancel()
			case <-sctx.Done():
			}
		}()
		err = c.session(sctx, cfg, acc)
		cancel()
		c.mu.Lock()
		c.acc = chatAccount{}
		c.mu.Unlock()

		switch {
		case ctx.Err() != nil:
			return
		case sctx.Err() != nil: // reload requested
			backoff = 5 * time.Second
		case errors.Is(err, errTwitchAuth):
			c.setStatus("error: the chat account's Twitch login expired, please connect it again")
			c.wait(ctx, 0)
		default:
			c.setStatus(fmt.Sprintf("error: %v (retrying)", err))
			c.wait(ctx, backoff)
			backoff = min(backoff*2, 2*time.Minute)
		}
	}
}

// wait blocks until settings change, the timeout passes (0 = no timeout) or ctx ends.
func (c *Chat) wait(ctx context.Context, d time.Duration) {
	var timeout <-chan time.Time
	if d > 0 {
		timeout = time.After(d)
	}
	select {
	case <-ctx.Done():
	case <-c.reload:
	case <-timeout:
	}
}

type eventsubMessage struct {
	Metadata struct {
		MessageType      string `json:"message_type"`
		SubscriptionType string `json:"subscription_type"`
	} `json:"metadata"`
	Payload struct {
		Session struct {
			ID               string `json:"id"`
			KeepaliveSeconds int    `json:"keepalive_timeout_seconds"`
			ReconnectURL     string `json:"reconnect_url"`
		} `json:"session"`
		Event chatEvent `json:"event"`
	} `json:"payload"`
}

type chatEvent struct {
	ChatterID   string `json:"chatter_user_id"`
	ChatterName string `json:"chatter_user_name"`
	MessageID   string `json:"message_id"`
	Message     struct {
		Text string `json:"text"`
	} `json:"message"`
	Badges []struct {
		SetID string `json:"set_id"`
	} `json:"badges"`
}

// session runs one EventSub connection, following Twitch's reconnect requests.
func (c *Chat) session(ctx context.Context, cfg Config, acc chatAccount) error {
	url, subscribed := eventsubURL, false
	for {
		conn, _, err := websocket.Dial(ctx, url, nil)
		if err != nil {
			return fmt.Errorf("connecting to Twitch chat: %w", err)
		}
		conn.SetReadLimit(1 << 20)
		next, err := c.read(ctx, conn, cfg, acc, &subscribed)
		conn.Close(websocket.StatusNormalClosure, "")
		if err != nil {
			return err
		}
		url = next // Twitch asked us to move; subscriptions carry over
	}
}

func (c *Chat) read(ctx context.Context, conn *websocket.Conn, cfg Config, acc chatAccount, subscribed *bool) (reconnectURL string, err error) {
	keepalive := 30 * time.Second
	for {
		rctx, cancel := context.WithTimeout(ctx, keepalive+10*time.Second)
		_, data, err := conn.Read(rctx)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			return "", fmt.Errorf("chat connection lost: %w", err)
		}
		var m eventsubMessage
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		switch m.Metadata.MessageType {
		case "session_welcome":
			if s := m.Payload.Session.KeepaliveSeconds; s > 0 {
				keepalive = time.Duration(s) * time.Second
			}
			if !*subscribed {
				if err := c.subscribe(ctx, cfg, acc, m.Payload.Session.ID); err != nil {
					return "", err
				}
				*subscribed = true
			}
			c.mu.Lock()
			c.acc = acc
			c.mu.Unlock()
			c.setStatus("connected as " + acc.login)
		case "session_reconnect":
			return m.Payload.Session.ReconnectURL, nil
		case "revocation":
			return "", errTwitchAuth
		case "notification":
			if m.Metadata.SubscriptionType == "channel.chat.message" {
				c.handle(ctx, cfg, acc, m.Payload.Event)
			}
		}
	}
}

func (c *Chat) subscribe(ctx context.Context, cfg Config, acc chatAccount, session string) error {
	body := map[string]any{
		"type":    "channel.chat.message",
		"version": "1",
		"condition": map[string]string{
			"broadcaster_user_id": cfg.TwitchUserID,
			"user_id":             acc.userID,
		},
		"transport": map[string]string{"method": "websocket", "session_id": session},
	}
	return c.twitch.doAs(ctx, acc.token, clientID(cfg), "POST", "/eventsub/subscriptions", body, nil)
}

func (c *Chat) handle(ctx context.Context, cfg Config, acc chatAccount, ev chatEvent) {
	if cfg.Chat.ReplyAs != "channel" && ev.ChatterID == acc.userID {
		return // the bot's own messages
	}
	words := strings.Fields(ev.Message.Text)
	if len(words) == 0 {
		return
	}
	var cmd *ChatCommand
	for i := range cfg.Chat.Commands {
		if cfg.Chat.Commands[i].Enabled && strings.EqualFold(words[0], cfg.Chat.Commands[i].Trigger) {
			cmd = &cfg.Chat.Commands[i]
			break
		}
	}
	if cmd == nil {
		return
	}

	privileged := false // the streamer and mods skip the cooldown
	for _, b := range ev.Badges {
		privileged = privileged || b.SetID == "broadcaster" || b.SetID == "moderator"
	}
	c.mu.Lock()
	if !privileged && time.Since(c.lastUsed[cmd.ID]) < time.Duration(cfg.Chat.CooldownSeconds)*time.Second {
		c.mu.Unlock()
		return
	}
	c.lastUsed[cmd.ID] = time.Now()
	c.mu.Unlock()

	vars := c.worker.ChatVars()
	vars["channel"], vars["user"] = cfg.TwitchLogin, ev.ChatterName
	tmpl := cmd.Reply
	switch {
	case vars["total"] == "":
		tmpl = cfg.Chat.NoGameReply
	case cmd.ID == "chasing" && vars["chasing"] == "":
		tmpl = "{channel} isn't chasing a particular achievement right now."
	}
	if msg := renderChat(tmpl, vars); msg != "" {
		c.send(ctx, cfg, acc, msg, ev.MessageID)
	}
}

// Announce posts an unlock message in chat (called by the worker).
func (c *Chat) Announce(unlocked []Achievement) {
	cfg := c.store.Get()
	c.mu.Lock()
	acc := c.acc
	c.mu.Unlock()
	if !cfg.Chat.Enabled || !cfg.Chat.AnnounceUnlocks || acc.token == "" {
		return
	}
	vars := c.worker.ChatVars()
	vars["channel"] = cfg.TwitchLogin
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, a := range unlocked {
		vars["latest"], vars["latest_rarity"] = a.Name, rarity(a.Percent)
		if msg := renderChat(cfg.Chat.AnnounceTemplate, vars); msg != "" {
			c.send(ctx, cfg, acc, msg, "")
		}
	}
}

func (c *Chat) send(ctx context.Context, cfg Config, acc chatAccount, msg, replyTo string) {
	body := map[string]string{"broadcaster_id": cfg.TwitchUserID, "sender_id": acc.userID, "message": msg}
	if replyTo != "" {
		body["reply_parent_message_id"] = replyTo
	}
	var r struct {
		Data []struct {
			IsSent     bool `json:"is_sent"`
			DropReason *struct {
				Message string `json:"message"`
			} `json:"drop_reason"`
		} `json:"data"`
	}
	if err := c.twitch.doAs(ctx, acc.token, clientID(cfg), "POST", "/chat/messages", body, &r); err != nil {
		log.Println("chat: sending message:", err)
		return
	}
	if len(r.Data) > 0 && !r.Data[0].IsSent && r.Data[0].DropReason != nil {
		log.Println("chat: Twitch didn't post the message:", r.Data[0].DropReason.Message)
	}
}

var parenGroupRe = regexp.MustCompile(`\s*\([^()]*\)`)

// renderChat fills a chat template. A "(…)" part whose placeholders are all
// empty is dropped, so "(only {next_rarity} of players)" disappears for games
// without rarity data instead of reading "(only  of players)".
func renderChat(tmpl string, vars map[string]string) string {
	tmpl = parenGroupRe.ReplaceAllStringFunc(tmpl, func(group string) string {
		names := placeholderRe.FindAllStringSubmatch(group, -1)
		if len(names) == 0 {
			return group
		}
		for _, n := range names {
			if vars[n[1]] != "" {
				return group
			}
		}
		return ""
	})
	out := placeholderRe.ReplaceAllStringFunc(tmpl, func(m string) string { return vars[m[1:len(m)-1]] })
	out = strings.Join(strings.Fields(out), " ")
	if r := []rune(out); len(r) > maxChatLen {
		out = string(r[:maxChatLen-1]) + "…"
	}
	return out
}

// cleanChatConfig tidies chat settings coming from the settings page.
func cleanChatConfig(in ChatConfig) ChatConfig {
	if in.ReplyAs != "channel" {
		in.ReplyAs = "bot"
	}
	if in.CooldownSeconds < 5 {
		in.CooldownSeconds = 5
	}
	var cmds []ChatCommand
	for _, cmd := range in.Commands {
		cmd.Trigger = strings.ToLower(strings.Join(strings.Fields(cmd.Trigger), ""))
		if r := []rune(cmd.Reply); len(r) > maxChatLen {
			cmd.Reply = string(r[:maxChatLen])
		}
		if cmd.ID != "" && cmd.Trigger != "" {
			cmds = append(cmds, cmd)
		}
	}
	in.Commands = cmds
	in.fillDefaults()
	return in
}
