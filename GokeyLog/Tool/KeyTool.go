package KeyTool

import (
	"encoding/json"
	"fmt"
	hook "github.com/robotn/gohook"
	"os"            // 导入os包，提供了一个平台无关的操作系统API
	"path/filepath" // 导入filepath包，提供了一些操作文件路径的函数
	"time"
)

// GetAppPath 函数返回当前应用程序的路径
// GetAppPath function returns the path of the current application
func GetAppPath() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		exePath, err = os.Getwd() // 获取当前工作目录 Get the current working directory
		if err != nil {
			return "", err // 如果获取失败，返回错误 Return error if failed to get
		}
	}
	return filepath.Dir(exePath), nil // 返回当前应用程序的路径 Return the path of the current application
}

// GetScriptDir 函数返回当前脚本的路径
// GetScriptDir function returns the path of the current script
func GetScriptDir() (path string, err error) {
	scriptpath, err := GetAppPath() // 获取当前应用程序的路径 Get the path of the current application
	if err != nil {
		return "", err // 如果获取失败，返回错误 Return error if failed to get
	}
	scriptpath = filepath.Join(scriptpath, "script") // 将脚本文件夹路径与应用程序路径拼接 Join the script folder path with the application path
	_, err = os.Stat(scriptpath)                     // 获取脚本文件夹的信息 Get information about the script folder
	if os.IsNotExist(err) {                          // 如果脚本文件夹不存在 If the script folder does not exist
		err = os.MkdirAll(scriptpath, os.ModePerm) // 创建脚本文件夹 Create the script folder
		if err != nil {
			return "", err // 如果创建失败，返回错误 Return error if failed to create
		}
	}
	return scriptpath, nil // 返回脚本文件夹路径 Return the path of the script folder
}
func KeyLog() {

	evChan := hook.Start()
	defer hook.End()
	scriptpath, _ := GetScriptDir()
	scriptpath = filepath.Join(scriptpath, time.Now().Format("2006-01-02-150405")+".script")
	fmt.Println("Path: " + scriptpath)
	file, err := os.OpenFile(scriptpath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer file.Close()
	// Hook所有支持的事件
	// case hook.HookEnabled: 钩子启用
	// case hook.HookDisabled: 钩子禁用
	// case hook.KeyDown: 按键按下
	// case hook.KeyUp: 按键释放
	// case hook.KeyHold: 按键持续按下
	// case hook.MouseMove: 鼠标移动
	// case hook.MouseDrag: 鼠标拖拽
	// case hook.MouseUp: 鼠标释放
	// case hook.MouseDown: 鼠标按下
	// case hook.MouseClick: 鼠标单击
	// case hook.MouseDblClick: 鼠标双击
	// case hook.MouseWheel: 鼠标滚轮
	// case hook.MouseHWheel: 鼠标横向滚轮
	// case hook.MouseHold: 鼠标持续按下
	// case hook.MouseRelease: 鼠标释放
	// The structure of the events is defined by the hook.Event struct, which has the following fields:
	// Kind: 事件类型（例如KeyDown，MouseMove等）
	// Rawcode: 按下的键的原始代码（仅适用于键盘事件）
	// Keychar: 按下的键的字符表示（仅适用于键盘事件）
	// Button: 按下的鼠标按钮（仅适用于鼠标事件）
	// X: 鼠标光标的x坐标（仅适用于鼠标事件）
	// Y: 鼠标光标的y坐标（仅适用于鼠标事件）
	// Clicks: 点击次数（仅适用于鼠标事件）
	// Amount: 滚动量（仅适用于鼠标滚轮事件）
	// Rotation: 鼠标滚轮的旋转（仅适用于鼠标滚轮事件）
	// Direction: 鼠标滚轮的方向（仅适用于鼠标滚轮事件）

	// 事件的支持操作函数有：
	// hook.Register：为特定事件（例如KeyDown，MouseMove等）注册新的钩子
	// hook.Start：开始监听事件
	// hook.End：停止监听事件
	// hook.AddEvents：添加多个要监听的事件
	// hook.AddEvent：添加单个要监听的事件

	for ev := range evChan {
		switch ev.Kind {
		case hook.HookEnabled:
			fmt.Println("Event: {Kind: HookEnabled}")
		case hook.HookDisabled:
			fmt.Println("Event: {Kind: HookDisabled}")
		case hook.KeyHold:
			fmt.Println("Event: {Kind: KeyHold, Rawcode: %v, Keychar: %v}", ev.Rawcode, ev.Keychar)
		case hook.MouseHold:
			fmt.Println("Event: {Kind: MouseHold, Button: %v, X: %v, Y: %v, Clicks: %v}", ev.Button, ev.X, ev.Y, ev.Clicks)
		case hook.MouseWheel:
			fmt.Println("Event: {Kind: MouseWheel, Amount: %v, Rotation: %v, Direction: %v}", ev.Amount, ev.Rotation, ev.Direction)
		case hook.FakeEvent:
			fmt.Println("Event: {Kind: FakeEvent}")
		default:
			fmt.Println(ev.String())

			modl, _ := json.Marshal(ev)
			_, err = file.WriteString(string(modl) + "\n")
			if err != nil {
				fmt.Println(err)
				return
			}
		}

	}
}
