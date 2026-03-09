package main

import (
	"context"
	"errors"
	"fmt"
	"sync"

	coreapp "auto-work/internal/app"
)

// App struct
type App struct {
	ctx        context.Context
	mu         sync.RWMutex
	backend    *coreapp.App
	startupErr error
	ready      chan struct{}
	readyOnce  sync.Once
}

var errBackendNotReady = errors.New("backend not initialized")

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{
		ready: make(chan struct{}),
	}
}

// Greet returns a greeting for the given name
func (a *App) Greet(name string) string {
	return fmt.Sprintf("Hello %s, It's show time!", name)
}
