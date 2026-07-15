package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// ShowDirectoryPicker 打开一个自定义目录选择器，允许用户浏览完整文件系统。
// 用户可以在任意层级选择目录，不受系统沙盒限制。
func ShowDirectoryPicker(parent fyne.Window, title string, callback func(string)) {
	// 从用户主目录或根目录开始
	startDir := "/"
	cwd, err := os.Getwd()
	if err == nil {
		startDir = cwd
	}
	home, err := os.UserHomeDir()
	if err == nil {
		startDir = home
	}

	_ShowDirectoryPickerAt(parent, title, startDir, callback)
}

func _ShowDirectoryPickerAt(parent fyne.Window, title string, currentDir string, callback func(string)) {
	// 路径输入框
	pathEntry := widget.NewEntry()
	pathEntry.SetText(currentDir)

	// 先声明 d 变量（注意是指针类型）
	var d dialog.Dialog

	// 目录列表
	var dirs []string
	var list *widget.List
	var parentDirBtn *widget.Button
	var selectLabel *widget.Label

	refreshDir := func(dir string) {
		dir = filepath.Clean(dir)
		pathEntry.SetText(dir)
		if selectLabel != nil {
			selectLabel.SetText("选中目录: " + dir)
		}

		entries, err := os.ReadDir(dir)
		if err != nil {
			dirs = nil
			list.Refresh()
			return
		}

		dirs = nil
		for _, e := range entries {
			if e.IsDir() {
				dirs = append(dirs, e.Name())
			}
		}
		sort.Strings(dirs)

		// 在列表顶部插入 ".." 以便返回上级目录（根目录除外）
		parent := filepath.Dir(dir)
		if parent != dir {
			dirs = append([]string{".."}, dirs...)
		}

		list.Refresh()
		if parentDirBtn != nil {
			parentDirBtn.Enable()
		}
	}

	list = widget.NewList(
		func() int { return len(dirs) },
		func() fyne.CanvasObject {
			return container.NewHBox(
				widget.NewIcon(theme.FolderIcon()),
				widget.NewLabel(""),
			)
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			box := o.(*fyne.Container)
			label := box.Objects[1].(*widget.Label)
			if dirs[i] == ".." {
				box.Objects[0].(*widget.Icon).SetResource(theme.FolderOpenIcon())
				label.SetText(".. (上级目录)")
			} else {
				box.Objects[0].(*widget.Icon).SetResource(theme.FolderIcon())
				label.SetText(dirs[i])
			}
		},
	)

	list.OnSelected = func(id widget.ListItemID) {
		if dirs[id] == ".." {
			refreshDir(filepath.Dir(pathEntry.Text))
		} else {
			refreshDir(filepath.Join(pathEntry.Text, dirs[id]))
		}
	}

	// "转到上级" 按钮
	parentDirBtn = widget.NewButtonWithIcon("上级目录", theme.MoveUpIcon(), func() {
		refreshDir(filepath.Dir(pathEntry.Text))
	})

	// 路径跳转功能
	pathEntry.OnSubmitted = func(s string) {
		info, err := os.Stat(s)
		if err == nil && info.IsDir() {
			refreshDir(s)
		}
	}

	// 快捷跳转到常用目录
	quickDirs := []struct {
		name string
		path string
	}{
		{"根目录 /", "/"},
		{"用户主目录", func() string {
			h, _ := os.UserHomeDir()
			return h
		}()},
		{"桌面", func() string {
			h, _ := os.UserHomeDir()
			return filepath.Join(h, "Desktop")
		}()},
	}
	quickBtns := make([]fyne.CanvasObject, 0, len(quickDirs))
	for _, qd := range quickDirs {
		if _, err := os.Stat(qd.path); err == nil {
			path := qd.path
			btn := widget.NewButton(qd.name, func() { refreshDir(path) })
			quickBtns = append(quickBtns, btn)
		}
	}

	// 当前选中信息
	selectLabel = widget.NewLabel("选中目录: " + currentDir)

	// "选择此目录" 按钮
	selectBtn := widget.NewButtonWithIcon("选择此目录", theme.ConfirmIcon(), func() {
		dir := filepath.Clean(pathEntry.Text)
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			dialog.ShowError(fmt.Errorf("所选路径不是有效目录"), parent)
			return
		}
		if callback != nil {
			callback(dir)
		}
		d.Hide() // ✅ 修复：关闭对话框
	})

	cancelBtn := widget.NewButton("取消", func() {})

	content := container.NewBorder(
		container.NewVBox(
			container.NewBorder(nil, nil, widget.NewLabel("路径:"), nil, pathEntry),
			container.NewHBox(append(quickBtns, parentDirBtn)...),
			widget.NewSeparator(),
		),
		container.NewVBox(
			widget.NewSeparator(),
			selectLabel,
			container.NewHBox(selectBtn, layout.NewSpacer(), cancelBtn), // 设置取消按钮的左右位置
		),
		nil, nil,
		container.NewVScroll(list),
	)

	// d = dialog.NewCustom(title, "", content, parent)
	d = dialog.NewCustomWithoutButtons(title, content, parent)
	d.Resize(fyne.NewSize(650, 520))

	// 重新绑定取消按钮
	cancelBtn.OnTapped = func() { d.Hide() }

	d.Show()
	refreshDir(currentDir)
}

// ShowFilePicker 打开文件选择器，支持浏览完整文件系统。
func ShowFilePicker(parent fyne.Window, title string, callback func(string)) {

	// 先声明 d 变量（注意是指针类型）
	var d dialog.Dialog

	currentDir := "/"
	home, err := os.UserHomeDir()
	if err == nil {
		currentDir = home
	}

	pathEntry := widget.NewEntry()
	pathEntry.SetText(currentDir)

	var entries []os.DirEntry
	var list *widget.List
	var selectLabel *widget.Label

	refreshDir := func(dir string) {
		dir = filepath.Clean(dir)
		pathEntry.SetText(dir)
		if selectLabel != nil {
			selectLabel.SetText("浏览目录: " + dir)
		}
		var err error
		entries, err = os.ReadDir(dir)
		if err != nil {
			entries = nil
			list.Refresh()
			return
		}
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].IsDir() != entries[j].IsDir() {
				return entries[i].IsDir()
			}
			return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name())
		})
		parent := filepath.Dir(dir)
		if parent != dir {
			entries = append([]os.DirEntry{&fakeDirEntry{name: "..", isDir: true}}, entries...)
		}
		list.Refresh()
	}

	list = widget.NewList(
		func() int { return len(entries) },
		func() fyne.CanvasObject {
			return container.NewHBox(
				widget.NewIcon(theme.FileIcon()),
				widget.NewLabel(""),
			)
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			box := o.(*fyne.Container)
			label := box.Objects[1].(*widget.Label)
			entry := entries[i]
			if entry.Name() == ".." {
				box.Objects[0].(*widget.Icon).SetResource(theme.FolderOpenIcon())
				label.SetText(".. (上级目录)")
			} else if entry.IsDir() {
				box.Objects[0].(*widget.Icon).SetResource(theme.FolderIcon())
				label.SetText(entry.Name() + "/")
			} else {
				box.Objects[0].(*widget.Icon).SetResource(theme.FileIcon())
				label.SetText(entry.Name())
			}
		},
	)

	list.OnSelected = func(id widget.ListItemID) {
		e := entries[id]
		if e.Name() == ".." {
			refreshDir(filepath.Dir(pathEntry.Text))
		} else if e.IsDir() {
			refreshDir(filepath.Join(pathEntry.Text, e.Name()))
		} else {
			// 选中文件
			filePath := filepath.Join(pathEntry.Text, e.Name())
			if callback != nil {
				callback(filePath)
			}
			d.Hide() // ✅ 修复：关闭对话框
		}
	}

	pathEntry.OnSubmitted = func(s string) {
		info, err := os.Stat(s)
		if err == nil && info.IsDir() {
			refreshDir(s)
		} else if err == nil && !info.IsDir() {
			if callback != nil {
				callback(s)
			}
			d.Hide() // ✅ 修复：路径直接输入文件时也关闭对话框
		}
	}

	parentDirBtn := widget.NewButtonWithIcon("上级目录", theme.MoveUpIcon(), func() {
		refreshDir(filepath.Dir(pathEntry.Text))
	})

	homePath, _ := os.UserHomeDir()
	quickBtns := []fyne.CanvasObject{
		widget.NewButton("根目录 /", func() { refreshDir("/") }),
		widget.NewButton("用户主目录", func() { refreshDir(homePath) }),
		parentDirBtn,
	}

	selectLabel = widget.NewLabel("选中文件: (无)")
	selectBtn := widget.NewButtonWithIcon("选择", theme.ConfirmIcon(), func() {
		dialog.ShowInformation("提示", "请在文件列表中双击选择一个文件，或点击目录进入后选择。", parent)
	})

	cancelBtn := widget.NewButton("取消", func() {})

	content := container.NewBorder(
		container.NewVBox(
			container.NewBorder(nil, nil, widget.NewLabel("路径:"), nil, pathEntry),
			container.NewHBox(quickBtns...),
			widget.NewSeparator(),
		),
		container.NewVBox(
			widget.NewSeparator(),
			selectLabel,
			container.NewHBox(selectBtn, layout.NewSpacer(), cancelBtn), // 设置取消按钮的靠右边的位置
		),
		nil, nil,
		container.NewVScroll(list),
	)

	d = dialog.NewCustomWithoutButtons(title, content, parent)
	d.Resize(fyne.NewSize(650, 520))
	cancelBtn.OnTapped = func() { d.Hide() }
	d.Show()
	refreshDir(currentDir)
}

// fakeDirEntry 用于在列表中插入 ".." 项
type fakeDirEntry struct {
	name  string
	isDir bool
}

func (f *fakeDirEntry) Name() string               { return f.name }
func (f *fakeDirEntry) IsDir() bool                { return f.isDir }
func (f *fakeDirEntry) Type() os.FileMode          { return os.ModeDir }
func (f *fakeDirEntry) Info() (os.FileInfo, error) { return nil, nil }
