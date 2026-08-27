package main

import (
	"codeql-assistant/ui"

	"fyne.io/fyne/v2/app"
)

func main() {
	// 打包后从 Finder/LaunchServices 启动时不会继承终端 PATH，启动时先补齐用户环境。
	initUserEnv()

	a := app.NewWithID("com.codeql.assistant")
	w := a.NewWindow("CodeQL Assistant")
	// fmt.Println(engine.AppDataDir())
	ui.NewApp(w)
	w.ShowAndRun()
}
