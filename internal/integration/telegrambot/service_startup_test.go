package telegrambot

import (
	"net/http"
	"strings"
	"testing"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func TestStartupNotifyTargetsSorted(t *testing.T) {
	targets := startupNotifyTargets(map[int64]struct{}{
		9:  {},
		3:  {},
		-1: {},
	})
	if len(targets) != 3 {
		t.Fatalf("unexpected target len: %d", len(targets))
	}
	if targets[0] != -1 || targets[1] != 3 || targets[2] != 9 {
		t.Fatalf("unexpected targets order: %#v", targets)
	}
}

func TestStartupNotifyMessageContainsHint(t *testing.T) {
	msg := startupNotifyMessage(time.Date(2026, 3, 4, 18, 20, 0, 0, time.Local))
	if !strings.Contains(msg, "自动工作台已启动") {
		t.Fatalf("missing startup text: %s", msg)
	}
	if !strings.Contains(msg, "/help") {
		t.Fatalf("missing help hint: %s", msg)
	}
}

func TestBuildHTTPClient_WithProxy(t *testing.T) {
	client, err := buildHTTPClient("http://127.0.0.1:7890")
	if err != nil {
		t.Fatalf("build client: %v", err)
	}
	if client == nil || client.Transport == nil {
		t.Fatalf("expected client transport")
	}
	tr, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport")
	}
	if tr.Proxy == nil {
		t.Fatalf("expected proxy function")
	}
}

func TestBuildHTTPClient_InvalidProxy(t *testing.T) {
	_, err := buildHTTPClient("://bad")
	if err == nil {
		t.Fatalf("expected error for invalid proxy")
	}
}

func TestBuildIncomingMessageLog_ContainsChatID(t *testing.T) {
	msg := &tgbotapi.Message{
		Text: "hello",
		Chat: &tgbotapi.Chat{
			ID:   123456,
			Type: "private",
		},
		From: &tgbotapi.User{
			UserName: "tester",
		},
	}
	line := buildIncomingMessageLog(msg)
	if !strings.Contains(line, "chat_id=123456") {
		t.Fatalf("log does not contain chat_id: %s", line)
	}
	if !strings.Contains(line, "from=@tester") {
		t.Fatalf("log does not contain sender: %s", line)
	}
}

func TestBuildIncomingMessageInfo_WithCommand(t *testing.T) {
	msg := &tgbotapi.Message{
		Text: "/help show",
		Entities: []tgbotapi.MessageEntity{
			{
				Type:   "bot_command",
				Offset: 0,
				Length: 5,
			},
		},
		Chat: &tgbotapi.Chat{
			ID:   778899,
			Type: "private",
		},
		From: &tgbotapi.User{
			FirstName: "Test",
			LastName:  "User",
		},
	}
	info := buildIncomingMessageInfo(msg)
	if info == nil {
		t.Fatalf("expected parsed incoming message")
	}
	if info.ChatID != 778899 {
		t.Fatalf("unexpected chat id: %d", info.ChatID)
	}
	if info.Command != "/help" {
		t.Fatalf("unexpected command: %q", info.Command)
	}
	if info.Text == "" {
		t.Fatalf("expected text in incoming info")
	}
}
