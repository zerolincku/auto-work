//go:build !darwin

package main

import "errors"

func showNativeSystemNotification(title, body string) error {
	_ = title
	_ = body
	return errors.New("native system notification unsupported on this platform")
}
