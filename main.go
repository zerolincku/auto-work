package main

import (
	"embed"
	"fmt"
	"os"
	"runtime"

	mcphttp "auto-work/internal/mcp/httpserver"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "mcp-http":
			if err := mcphttp.RunFromArgs(os.Args[2:]); err != nil {
				fmt.Fprintf(os.Stderr, "mcp-http failed: %v\n", err)
				os.Exit(1)
			}
			return
		}
	}

	// Create an instance of the app structure
	app := NewApp()
	macTitleBar := mac.TitleBarHiddenInset()
	macTitleBar.TitlebarAppearsTransparent = true
	macTitleBar.HideToolbarSeparator = true
	macTitleBar.UseToolbar = false

	// Create application with options
	err := wails.Run(&options.App{
		Title:            "auto-work",
		Width:            1200,
		Height:           820,
		MinWidth:         980,
		MinHeight:        700,
		Frameless:        runtime.GOOS == "windows",
		WindowStartState: options.Normal,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 0, G: 0, B: 0, A: 0},
		Mac: &mac.Options{
			TitleBar:             macTitleBar,
			Appearance:           mac.NSAppearanceNameVibrantLight,
			WebviewIsTransparent: true,
			WindowIsTranslucent:  true,
		},
		Windows: &windows.Options{
			WebviewIsTransparent:              true,
			WindowIsTranslucent:               true,
			BackdropType:                      windows.Mica,
			Theme:                             windows.SystemDefault,
			DisableWindowIcon:                 false,
			DisableFramelessWindowDecorations: false,
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
