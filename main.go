package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"os"

	"github.com/shaoxier/AIofCage/internal/runtime"
	"github.com/shaoxier/AIofCage/internal/service"
	"github.com/shaoxier/AIofCage/internal/store"
	"github.com/shaoxier/AIofCage/internal/vault"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/src
var assets embed.FS

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "AIofCage 启动失败:", err)
		os.Exit(1)
	}
}

func run() error {
	frontend, err := fs.Sub(assets, "frontend/src")
	if err != nil {
		return err
	}
	dir, err := store.DefaultDir()
	if err != nil {
		return err
	}
	st, err := store.Open(dir)
	if err != nil {
		return err
	}
	keychain := vault.NewKeychain()
	// Without Docker/Colima the app still opens: provider management works and the environment
	// page explains what is missing.
	manager, managerErr := runtime.NewManager(st, keychain)
	var desktopContext context.Context
	app := service.NewApp(st, keychain, manager, managerErr, func() (string, error) {
		if desktopContext == nil {
			return "", fmt.Errorf("桌面窗口尚未就绪")
		}
		return wailsruntime.OpenDirectoryDialog(desktopContext, wailsruntime.OpenDialogOptions{
			Title: "选择项目文件夹",
		})
	})
	return wails.Run(&options.App{
		Title: "AIofCage", Width: 1180, Height: 800, MinWidth: 900, MinHeight: 640,
		BackgroundColour: options.NewRGB(245, 246, 248),
		AssetServer:      &assetserver.Options{Assets: frontend},
		OnStartup:        func(ctx context.Context) { desktopContext = ctx },
		Bind:             []interface{}{app},
	})
}
