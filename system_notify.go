package main

import "strings"

func (a *App) showSystemNotification(title, body string) error {
	title = strings.TrimSpace(title)
	body = strings.TrimSpace(body)
	if title == "" && body == "" {
		return nil
	}
	return showNativeSystemNotification(title, body)
}
