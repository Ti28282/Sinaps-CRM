package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var icon []byte

func main() {
	app := NewApp()
	err := wails.Run(&options.App{
		Title:            "SINAPS CRM",
		Width:            1440,
		Height:           900,
		MinWidth:         1024,
		MinHeight:        680,
		AssetServer:      &assetserver.Options{Assets: assets, Handler: app.mediaHandler()},
		BackgroundColour: &options.RGBA{R: 13, G: 14, B: 17, A: 255},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind:             []interface{}{app},
		// перетаскивание файлов в окно: Wails отдаёт настоящие пути к файлам
		DragAndDrop: &options.DragAndDrop{EnableFileDrop: true, DisableWebViewDrop: true},
		// иконка окна и в доке; ProgramName = имя ярлыка sinaps-crm.desktop
		Linux: &linux.Options{Icon: icon, ProgramName: "sinaps-crm"},
	})
	if err != nil {
		log.Fatal(err)
	}
}
