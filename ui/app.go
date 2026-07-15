package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"codeql-assistant/engine"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const lastConfigFile = "last_config.json"

// App 持有完整的应用状态和 UI 控件。
type App struct {
	window  fyne.Window
	config  engine.CodeQLConfig
	runner  *engine.Runner
	history *engine.History

	binaryEntry   *widget.Entry
	queryEntry    *widget.Entry
	databaseEntry *widget.Entry
	sourceEntry   *widget.Entry
	languageSel   *widget.Select
	formatSel     *widget.Select
	outputEntry   *widget.Entry
	memoryEntry   *widget.Entry
	threadsEntry  *widget.Entry
	extraEntry    *widget.Entry
	profileSel    *widget.Select

	consoleLabel     *widget.Label
	consoleScroll    *container.Scroll
	statusLabel      *widget.Label
	progressBar      *widget.ProgressBarInfinite
	runBtn           *widget.Button
	stopBtn          *widget.Button
	clearBtn         *widget.Button
	saveProfileBtn   *widget.Button
	deleteProfileBtn *widget.Button // 新增删除配置按钮的功能 2026.7.15
	historyBtn       *widget.Button

	startTime time.Time
}

// NewApp 创建并初始化应用程序界面。
func NewApp(w fyne.Window) *App {
	a := &App{
		window:  w,
		config:  engine.DefaultConfig(),
		runner:  engine.NewRunner(),
		history: engine.NewHistory(),
	}

	a.runner.SetStatusCallback(func(s engine.RunStatus) {
		fyne.Do(func() {
			a.statusLabel.SetText(s.String())
		})
	})

	a.buildUI()
	a.loadLastConfig()
	a.syncUIFromConfig()
	a.updateProfileList()

	// 为控制台 Entry 应用自定义主题，使禁用状态下的文字仍保持明亮可读

	return a
}

func (a *App) buildUI() {
	a.createWidgets()
	configSection := a.buildConfigSection()
	consoleSection := a.buildConsoleSection()
	controlBar := a.buildControlBar()

	split := container.NewVSplit(configSection, consoleSection)
	split.SetOffset(0.61)

	content := container.NewBorder(nil, controlBar, nil, nil, split)
	a.window.SetContent(content)
	a.window.Resize(fyne.NewSize(1150, 800))
	a.window.SetTitle("CodeQL Assistant - by shiyan")
}

func (a *App) onDeleteProfile() { // 用于配置文件的删除使用 2026.7.15
	name := a.profileSel.Selected
	if name == "" {
		dialog.ShowInformation("提示", "请先选择一个要删除的配置文件。", a.window)
		return
	}

	confirmDialog := dialog.NewConfirm("删除配置",
		fmt.Sprintf("确定要删除配置文件「%s」吗？\n此操作不可恢复。", name),
		func(ok bool) {
			if !ok {
				return
			}
			if err := engine.DeleteProfile(name); err != nil {
				dialog.ShowError(fmt.Errorf("删除失败: %w", err), a.window)
				return
			}
			a.updateProfileList()
			a.profileSel.ClearSelected()
			a.deleteProfileBtn.Disable()
			dialog.ShowInformation("已删除", fmt.Sprintf("配置文件「%s」已删除。", name), a.window)
		}, a.window)

	// 设置按钮文本：确认=是，取消=否 2026.7.15
	confirmDialog.SetConfirmText("是")
	confirmDialog.SetDismissText("否")
	confirmDialog.Show()

}

func (a *App) createWidgets() {
	a.binaryEntry = widget.NewEntry()
	a.binaryEntry.SetPlaceHolder("CodeQL 可执行文件的路径（例如 /usr/local/bin/codeql）。")

	a.queryEntry = widget.NewEntry()
	a.queryEntry.SetPlaceHolder(".ql 文件路径、查询套件（query suite）或查询包（query pack）目录的路径。")

	a.databaseEntry = widget.NewEntry()
	a.databaseEntry.SetPlaceHolder("CodeQL 数据库目录的路径。")

	a.sourceEntry = widget.NewEntry()
	a.sourceEntry.SetPlaceHolder("源码根目录（用于创建数据库）。")

	a.languageSel = widget.NewSelect(engine.SupportedLanguages(), func(v string) {
		a.config.Language = v
	})
	a.languageSel.SetSelected("java")

	a.formatSel = widget.NewSelect(engine.SupportedFormats(), func(v string) {
		a.config.OutputFormat = v
	})
	a.formatSel.SetSelected("sarifv2.1.0")

	a.outputEntry = widget.NewEntry()
	a.outputEntry.SetPlaceHolder("输出文件路径（例如 results.sarif）。")

	a.memoryEntry = widget.NewEntry()
	a.memoryEntry.SetText("4096")

	a.threadsEntry = widget.NewEntry()
	a.threadsEntry.SetText("4")

	a.extraEntry = widget.NewEntry()
	a.extraEntry.SetPlaceHolder("CodeQL 附加参数（例如 --no-sandbox --ram 8192）。")

	a.consoleLabel = widget.NewLabel("")
	a.consoleLabel.Wrapping = fyne.TextWrapBreak
	a.consoleLabel.TextStyle = fyne.TextStyle{Monospace: true, Bold: true}

	a.statusLabel = widget.NewLabel("就绪")
	a.statusLabel.TextStyle = fyne.TextStyle{Bold: true}

	a.progressBar = widget.NewProgressBarInfinite()
	a.progressBar.Hide()

	a.runBtn = widget.NewButtonWithIcon("开始分析", theme.MediaPlayIcon(), a.onRun)
	a.runBtn.Importance = widget.HighImportance

	a.stopBtn = widget.NewButtonWithIcon("强制停止", theme.MediaStopIcon(), a.onStop)
	a.stopBtn.Disable()

	a.clearBtn = widget.NewButtonWithIcon("清理输出", theme.ContentClearIcon(), func() {
		a.consoleLabel.SetText("")
		a.consoleLabel.Refresh() // 增加控制台内容刷新，避免出现白板 2026.7.15
		if a.consoleScroll != nil {
			a.consoleScroll.Refresh()
		}
	})

	a.saveProfileBtn = widget.NewButtonWithIcon("保存", theme.DocumentSaveIcon(), a.onSaveProfile)

	a.historyBtn = widget.NewButtonWithIcon("历史记录", theme.ListIcon(), a.onShowHistory)

	// 在配置文件前面增加删除配置的功能 2026.7.15
	a.deleteProfileBtn = widget.NewButtonWithIcon("删除", theme.DeleteIcon(), a.onDeleteProfile)
	a.deleteProfileBtn.Disable() // 未选择配置时禁用删除
	a.profileSel = widget.NewSelect([]string{}, func(v string) {
		if v != "" {
			a.loadProfile(v)
			a.deleteProfileBtn.Enable()
		} else {
			a.deleteProfileBtn.Disable()
		}
	})
	a.profileSel.PlaceHolder = "加载配置文件…"

	a.binaryEntry.OnChanged = func(s string) { a.config.BinaryPath = s }
	a.queryEntry.OnChanged = func(s string) { a.config.QueryPath = s }
	a.databaseEntry.OnChanged = func(s string) { a.config.DatabasePath = s }
	a.sourceEntry.OnChanged = func(s string) { a.config.SourceRoot = s }
	a.outputEntry.OnChanged = func(s string) { a.config.OutputFile = s }
	a.memoryEntry.OnChanged = func(s string) {
		if v, err := strconv.Atoi(s); err == nil {
			a.config.RamLimit = v
		}
	}
	a.threadsEntry.OnChanged = func(s string) {
		if v, err := strconv.Atoi(s); err == nil {
			a.config.Threads = v
		}
	}
	a.extraEntry.OnChanged = func(s string) { a.config.ExtraArgs = s }
}

func (a *App) buildConfigSection() fyne.CanvasObject {
	header := widget.NewLabelWithStyle("工具路径设置", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})

	makeLabel := func(text string) *widget.Label { return widget.NewLabel(text) }

	rowWithBtn := func(label string, entry *widget.Entry, btnText string, icon fyne.Resource, action func()) fyne.CanvasObject {
		btn := widget.NewButtonWithIcon(btnText, icon, action)
		return container.NewBorder(nil, nil, makeLabel(label), btn, entry)
	}

	rowNoBtn := func(label string, entry *widget.Entry) fyne.CanvasObject {
		return container.NewBorder(nil, nil, makeLabel(label), nil, entry)
	}

	// 二进制文件选择 — 使用自定义文件浏览器，支持完整文件系统导航
	binaryRow := rowWithBtn("CodeQL 二进制文件:", a.binaryEntry, "打开", theme.FileIcon(), func() {
		ShowFilePicker(a.window, "选择 CodeQL 二进制文件", func(path string) {
			a.binaryEntry.SetText(path)
		})
	})

	// 查询/套件选择 — 使用自定义目录浏览器
	queryRow := rowWithBtn("查询 / 套件:", a.queryEntry, "打开", theme.FolderOpenIcon(), func() {
		ShowDirectoryPicker(a.window, "选择查询目录", func(path string) {
			a.queryEntry.SetText(path)
		})
	})

	// 数据库目录选择 — 使用自定义目录浏览器
	dbRow := rowWithBtn("数据库:", a.databaseEntry, "打开", theme.FolderOpenIcon(), func() {
		ShowDirectoryPicker(a.window, "选择数据库目录", func(path string) {
			a.databaseEntry.SetText(path)
		})
	})

	// 源码根目录选择 — 使用自定义目录浏览器
	srcRow := rowWithBtn("源码根目录:", a.sourceEntry, "打开", theme.FolderOpenIcon(), func() {
		ShowDirectoryPicker(a.window, "选择源码根目录", func(path string) {
			a.sourceEntry.SetText(path)
		})
	})

	langFmtRow := container.NewGridWithColumns(2,
		container.NewBorder(nil, nil, makeLabel("编程语言:"), nil, a.languageSel),
		container.NewBorder(nil, nil, makeLabel("格式:"), nil, a.formatSel),
	)

	outRow := rowWithBtn("输出目录及文件:", a.outputEntry, "另存为", theme.FileIcon(), func() {
		dialog.ShowFileSave(func(w fyne.URIWriteCloser, err error) {
			if err == nil && w != nil {
				a.outputEntry.SetText(w.URI().Path())
			}
		}, a.window)
	})

	memRow := container.NewGridWithColumns(4,
		makeLabel("内存限额（MB）:"), a.memoryEntry,
		makeLabel("线程数:"), a.threadsEntry,
	)

	extraRow := rowNoBtn("附加参数:", a.extraEntry)

	profileRow := container.NewBorder(nil, nil,
		makeLabel("参数配置:"), container.NewHBox(a.saveProfileBtn, a.deleteProfileBtn), a.profileSel,
	) // 在保存配置的旁边，增加删除按钮 2026.7.15

	createDBBtn := widget.NewButtonWithIcon("创建数据库", theme.StorageIcon(), a.onCreateDatabase)
	FamousQuotes := engine.GetRandomQuote()

	actionsRow := container.NewHBox(
		createDBBtn,
		a.historyBtn,
		layout.NewSpacer(),
		widget.NewLabel(FamousQuotes), // 用于每次开启时，显示一些经典语录 2026.7.15
	)

	form := container.NewVBox(
		header,
		widget.NewSeparator(),
		binaryRow,
		queryRow,
		dbRow,
		srcRow,
		actionsRow,
		widget.NewSeparator(),
		langFmtRow,
		outRow,
		memRow,
		extraRow,
		widget.NewSeparator(),
		profileRow,
	)

	return container.NewVScroll(form)
}

func (a *App) buildConsoleSection() fyne.CanvasObject {
	header := container.NewHBox(
		widget.NewLabelWithStyle("控制台输出", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		layout.NewSpacer(),
		a.clearBtn,
	)
	// 给控制台 Label 加一个卡片背景，模拟 Entry 的输入框视觉效果
	bg := canvas.NewRectangle(theme.InputBackgroundColor())
	bg.CornerRadius = theme.InputRadiusSize()
	// 设置背景最小尺寸，确保即使 Label 为空，背景也不会塌陷
	bg.SetMinSize(fyne.NewSize(100, 200))
	// 用 Padded 给文字留边距，再用 Stack 把背景和文字叠在一起
	card := container.NewStack(bg, container.NewPadded(a.consoleLabel))
	a.consoleScroll = container.NewVScroll(card)
	return container.NewBorder(header, nil, nil, nil, a.consoleScroll)
}
func (a *App) buildControlBar() fyne.CanvasObject {
	return container.NewBorder(
		nil, nil,
		container.NewHBox(
			a.runBtn,
			a.stopBtn,
			widget.NewSeparator(),
			a.statusLabel,
		),
		a.progressBar,
		nil,
	)
}

// --- 事件处理程序 ---
func (a *App) onRun() {
	a.syncConfigFromUI() // 增加默认参数，如 内存、线程 的前置加载机制 2026.7.15
	if a.config.BinaryPath == "" {
		dialog.ShowError(fmt.Errorf("请选择 CodeQL 二进制文件路径"), a.window)
		return
	}
	if a.config.DatabasePath == "" {
		dialog.ShowError(fmt.Errorf("请选择一个 CodeQL 数据库"), a.window)
		return
	}
	if _, err := os.Stat(a.config.BinaryPath); err != nil {
		dialog.ShowError(fmt.Errorf("指定的路径处未找到 CodeQL 二进制文件: %s", a.config.BinaryPath), a.window)
		return
	}

	a.saveLastConfig()
	a.consoleLabel.SetText("")
	a.runBtn.Disable()
	a.stopBtn.Enable()
	a.progressBar.Show()
	a.progressBar.Start()
	a.startTime = time.Now()

	go func() {
		defer func() {
			fyne.Do(func() {
				a.runBtn.Enable()
				a.stopBtn.Disable()
				a.progressBar.Hide()
				a.progressBar.Stop()
			})
		}()

		cfg := a.config
		err := a.runner.RunAnalyze(cfg, func(line string, isError bool) {
			lineCopy := line
			fyne.Do(func() {
				a.appendOutput(lineCopy)
			})
		})

		duration := time.Since(a.startTime)
		status := "已完成"
		if err != nil {
			status = a.runner.Status().String()
		}
		a.history.AddEntry(cfg, status, duration, cfg.OutputFile)
	}()
}

func (a *App) onStop() {
	a.appendOutput("--- Stopping... ---")
	a.runner.Stop()
}

func (a *App) onCreateDatabase() {
	a.syncConfigFromUI() // 增加默认参数，如 内存、线程 的前置加载机制 2026.7.15
	if a.config.BinaryPath == "" {
		dialog.ShowError(fmt.Errorf("请先选择 CodeQL 二进制文件路径"), a.window)
		return
	}
	if a.config.SourceRoot == "" {
		dialog.ShowError(fmt.Errorf("请指定源码根目录"), a.window)
		return
	}
	if a.config.DatabasePath == "" {
		dialog.ShowError(fmt.Errorf("请指定数据库输出路径"), a.window)
		return
	}

	confirmDialog := dialog.NewConfirm("创建数据库",
		fmt.Sprintf("创建 CodeQL 数据库:\n\n源码目录: %s\n编程语言: %s\n文件输出: %s",
			a.config.SourceRoot, a.config.Language, a.config.DatabasePath),
		func(ok bool) {
			if !ok {
				return
			}
			a.saveLastConfig()
			a.consoleLabel.SetText("")
			a.runBtn.Disable()
			a.stopBtn.Enable()
			a.progressBar.Show()
			a.progressBar.Start()
			a.startTime = time.Now()

			go func() {
				defer func() {
					fyne.Do(func() {
						a.runBtn.Enable()
						a.stopBtn.Disable()
						a.progressBar.Hide()
						a.progressBar.Stop()
					})
				}()

				cfg := a.config
				err := a.runner.RunDatabaseCreate(cfg, func(line string, isError bool) {
					lineCopy := line
					fyne.Do(func() {
						a.appendOutput(lineCopy)
					})
				})

				duration := time.Since(a.startTime)
				status := "数据库已创建"
				if err != nil {
					status = "数据库创建失败"
				}
				a.history.AddEntry(cfg, status, duration, cfg.DatabasePath)
			}()
		}, a.window)
	confirmDialog.SetConfirmText("是") // 修改选择按钮为是和否 2026.7.15
	confirmDialog.SetDismissText("否")
	confirmDialog.Show()
}

func (a *App) onSaveProfile() {
	a.syncConfigFromUI()

	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("eg: java-analysis")
	nameEntry.TextStyle = fyne.TextStyle{Monospace: true}

	// 当前配置预览
	var lang, query, db, src, output string
	if a.config.Language != "" {
		lang = fmt.Sprintf("语言: %s  格式: %s", a.config.Language, a.config.OutputFormat)
	}
	if a.config.QueryPath != "" {
		query = fmt.Sprintf("查询: %s", a.config.QueryPath)
	}
	if a.config.DatabasePath != "" {
		db = fmt.Sprintf("数据库: %s", a.config.DatabasePath)
	}
	if a.config.SourceRoot != "" {
		src = fmt.Sprintf("源码: %s", a.config.SourceRoot)
	}
	if a.config.OutputFile != "" {
		output = fmt.Sprintf("输出: %s", a.config.OutputFile)
	}
	previewLines := []string{lang, query, db, src, output}
	var nonEmpty []string
	for _, l := range previewLines {
		if l != "" {
			nonEmpty = append(nonEmpty, l)
		}
	}
	previewStr := strings.Join(nonEmpty, "\n")
	preview := widget.NewLabel(previewStr)
	preview.Wrapping = fyne.TextWrapBreak
	preview.TextStyle = fyne.TextStyle{Monospace: true}

	// 操作按钮
	saveBtn := widget.NewButtonWithIcon("保存", theme.DocumentSaveIcon(), func() {})
	cancelBtn := widget.NewButton("取消", func() {})

	content := container.NewBorder(
		container.NewVBox(
			widget.NewLabelWithStyle("保存当前配置", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			widget.NewSeparator(),
			widget.NewForm(
				&widget.FormItem{Text: "配置文件名称", Widget: nameEntry},
			),
			widget.NewSeparator(),
			widget.NewLabelWithStyle("当前配置快照", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			preview,
		),
		container.NewHBox(saveBtn, layout.NewSpacer(), cancelBtn),
		nil, nil,
	)

	// d := dialog.NewCustom("保存配置文件", "", content, a.window)
	d := dialog.NewCustomWithoutButtons("保存配置文件", content, a.window)
	d.Resize(fyne.NewSize(500, 420))

	saveBtn.OnTapped = func() {
		if nameEntry.Text == "" {
			dialog.ShowError(fmt.Errorf("请输入配置文件名称"), a.window)
			return
		}
		a.config.Name = nameEntry.Text
		path := engine.ProfilePath(nameEntry.Text)
		if err := a.config.SaveToFile(path); err != nil {
			dialog.ShowError(err, a.window)
			return
		}
		a.updateProfileList()
		a.profileSel.SetSelected(nameEntry.Text)
		d.Hide()
		dialog.ShowInformation("已保存", fmt.Sprintf("配置文件「%s」保存成功。", nameEntry.Text), a.window)
	}
	cancelBtn.OnTapped = func() { d.Hide() }

	d.Show()
}

func (a *App) onShowHistory() { // 增加清空历史记录按钮的功能 2026.7.15
	if len(a.history.Entries) == 0 {
		dialog.ShowInformation("历史记录", "暂无运行记录。", a.window)
		return
	}

	var items []string
	for _, e := range a.history.Entries {
		ts := e.Timestamp.Format("2006-01-02 15:04")
		dur := time.Duration(e.DurationMs) * time.Millisecond
		item := fmt.Sprintf("[%s]  %s  —  %s  (%s)",
			ts, e.Status, e.Config.Language, dur.Round(time.Second))
		items = append(items, item)
	}

	list := widget.NewList(
		func() int { return len(items) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			o.(*widget.Label).SetText(items[i])
		},
	)

	// 新增：清空历史按钮
	clearBtn := widget.NewButtonWithIcon("清空历史", theme.DeleteIcon(), nil)
	closeBtn := widget.NewButton("关闭", nil)

	btnBar := container.NewHBox(clearBtn, layout.NewSpacer(), closeBtn)

	content := container.NewBorder(nil, btnBar, nil, nil, container.NewVScroll(list))

	d := dialog.NewCustomWithoutButtons("运行历史", content, a.window)
	d.Resize(fyne.NewSize(550, 400))

	// 清空按钮事件：二次确认后清空
	clearBtn.OnTapped = func() {
		// app.go 中 onShowHistory 里的清空确认对话框
		confirmDialog := dialog.NewConfirm("确认清空",
			"确定要清空所有历史记录吗？\n此操作不可恢复。",
			func(ok bool) {
				if !ok {
					return
				}
				a.history.Clear()
				d.Hide()
				dialog.ShowInformation("已清空", "历史记录已清空。", a.window)
			}, a.window)
		confirmDialog.SetConfirmText("是")
		confirmDialog.SetDismissText("否")
		confirmDialog.Show()
	}

	closeBtn.OnTapped = func() { d.Hide() }

	d.Show()
}

func (a *App) loadProfile(name string) {
	path := engine.ProfilePath(name)
	cfg, err := engine.LoadConfigFromFile(path)
	if err != nil {
		dialog.ShowError(err, a.window)
		return
	}
	a.config = cfg
	a.syncUIFromConfig()
}

func (a *App) updateProfileList() {
	profiles := engine.ListProfiles()
	a.profileSel.Options = profiles
	a.profileSel.Refresh()
}

// --- Output ---

func (a *App) appendOutput(line string) {
	current := a.consoleLabel.Text
	if current != "" {
		current += "\n"
	}
	current += line
	a.consoleLabel.SetText(current)
	a.consoleLabel.Refresh()
	// 每次追加输出后，自动滚动到底部
	if a.consoleScroll != nil {
		a.consoleScroll.ScrollToBottom()
	}
}

// --- Config sync ---

func (a *App) syncConfigFromUI() {
	if v, err := strconv.Atoi(a.memoryEntry.Text); err == nil {
		a.config.RamLimit = v
	}
	if v, err := strconv.Atoi(a.threadsEntry.Text); err == nil {
		a.config.Threads = v
	}
}

func (a *App) syncUIFromConfig() {
	a.binaryEntry.SetText(a.config.BinaryPath)
	a.queryEntry.SetText(a.config.QueryPath)
	a.databaseEntry.SetText(a.config.DatabasePath)
	a.sourceEntry.SetText(a.config.SourceRoot)
	a.languageSel.SetSelected(a.config.Language)
	a.formatSel.SetSelected(a.config.OutputFormat)
	a.outputEntry.SetText(a.config.OutputFile)
	a.memoryEntry.SetText(strconv.Itoa(a.config.RamLimit))
	a.threadsEntry.SetText(strconv.Itoa(a.config.Threads))
	a.extraEntry.SetText(a.config.ExtraArgs)
}

func (a *App) saveLastConfig() {
	a.syncConfigFromUI()
	dir := engine.AppDataDir()
	os.MkdirAll(dir, 0755)
	path := filepath.Join(dir, lastConfigFile)
	a.config.SaveToFile(path)
}

func (a *App) loadLastConfig() {
	path := filepath.Join(engine.AppDataDir(), lastConfigFile)
	cfg, err := engine.LoadConfigFromFile(path)
	if err != nil {
		return
	}
	a.config = cfg
}
