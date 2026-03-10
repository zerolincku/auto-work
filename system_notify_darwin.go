//go:build darwin

package main

import (
	"fmt"
	"os/exec"
	"strings"
)

func showNativeSystemNotification(title, body string) error {
	title = strings.TrimSpace(title)
	body = strings.TrimSpace(body)
	if title == "" {
		title = "auto-work"
	}

	script := `on run argv
set theTitle to item 1 of argv
set theBody to item 2 of argv
display notification theBody with title theTitle
end run`

	cmd := exec.Command("osascript", "-e", script, title, body)
	output, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(output))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("show macos notification: %s", msg)
	}
	return nil
}
