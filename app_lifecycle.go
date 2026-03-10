package main

import (
	"context"
	"fmt"
	"time"

	coreapp "auto-work/internal/app"
	"auto-work/internal/config"
	"auto-work/internal/integration/telegrambot"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	backend, err := coreapp.New(ctx, config.Load())
	if err != nil {
		a.mu.Lock()
		a.startupErr = fmt.Errorf("initialize backend failed: %w", err)
		a.mu.Unlock()
		a.markReady()
		return
	}
	backend.SetTelegramIncomingReporter(func(message telegrambot.IncomingMessage) {
		runtime.EventsEmit(ctx, "telegram.incoming", message)
	})
	backend.SetFrontendRunReporter(func(notification coreapp.FrontendRunNotification) {
		runtime.EventsEmit(ctx, "run.notification", notification)
	})
	a.mu.Lock()
	a.backend = backend
	a.startupErr = nil
	a.mu.Unlock()
	a.markReady()
}

func (a *App) shutdown(ctx context.Context) {
	_ = ctx
	a.markReady()
	a.mu.RLock()
	backend := a.backend
	a.mu.RUnlock()
	if backend != nil {
		_ = backend.Close()
	}
}

func (a *App) backendOrErr() (*coreapp.App, error) {
	a.mu.RLock()
	backend := a.backend
	startupErr := a.startupErr
	ready := a.ready
	a.mu.RUnlock()
	if backend != nil {
		return backend, nil
	}
	if startupErr != nil {
		return nil, startupErr
	}
	if ready == nil {
		return nil, errBackendNotReady
	}

	timer := time.NewTimer(8 * time.Second)
	defer timer.Stop()
	select {
	case <-ready:
		a.mu.RLock()
		backend = a.backend
		startupErr = a.startupErr
		a.mu.RUnlock()
		if backend != nil {
			return backend, nil
		}
		if startupErr != nil {
			return nil, startupErr
		}
		return nil, errBackendNotReady
	case <-timer.C:
		return nil, errBackendNotReady
	}
}

func (a *App) markReady() {
	a.readyOnce.Do(func() {
		if a.ready != nil {
			close(a.ready)
		}
	})
}
