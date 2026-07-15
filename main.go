package main

import (
	"codeql-assistant/ui"
	"os"
	"path/filepath"

	"fyne.io/fyne/v2/app"
)

func ensureMavenInPath() {
	// 获取当前的 PATH
	currentPath := os.Getenv("PATH")
	// Homebrew 的典型安装路径（根据您的实际路径调整）
	brewPaths := []string{
		"/opt/homebrew/bin", // Apple Silicon Mac
		"/usr/local/bin",    // Intel Mac
		"/usr/bin",
		"/bin",
	}

	// 构建新的 PATH
	newPath := currentPath
	for _, p := range brewPaths {
		if _, err := os.Stat(p); err == nil {
			// 如果路径存在且尚未在 PATH 中，则添加
			if !pathInEnv(p, currentPath) {
				newPath = p + ":" + newPath
			}
		}
	}

	os.Setenv("PATH", newPath)
}

func pathInEnv(path, envPath string) bool {
	// 简单检查 PATH 中是否已包含该路径
	for _, p := range filepath.SplitList(envPath) {
		if p == path {
			return true
		}
	}
	return false
}

func main() {
	ensureMavenInPath() // 用于解决生成二进制文件后，无法正常识别mvn路径问题 2026.7.15
	a := app.NewWithID("com.codeql.assistant")
	w := a.NewWindow("CodeQL Assistant")
	// fmt.Println(engine.AppDataDir())
	ui.NewApp(w)
	w.ShowAndRun()
}
