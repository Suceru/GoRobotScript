package CallGo

import (
	KeyTool "GoRobotScript/GokeyLog/Tool"
	"github.com/go-vgo/robotgo"
	"github.com/yuin/gopher-lua"
)

func Loader(L *lua.LState) int {
	// register functions to the table
	mod := L.SetFuncs(L.NewTable(), exports)
	// register other stuff
	L.SetField(mod, "name", lua.LString("value"))

	// returns the module
	L.Push(mod)
	return 1
}

// 定义一个map类型的变量exports，其中key为string类型，value为lua.LGFunction类型
var exports = map[string]lua.LGFunction{
	"myfunc":    myfunc, // 将myfunc函数注册到exports中
	"keyLog":    keyLog, // 将keyLog函数注册到exports中
	"showalert": showalert,
}

func keyLog(L *lua.LState) int {
	KeyTool.KeyLog()
	return 0
}

// 定义showalert函数，该函数接收两个参数，分别为title和msg
// title和msg均为string类型
// 函数返回值为int类型
func showalert(L *lua.LState) int {
	// 将第一个参数转换为string类型
	title := L.ToString(1)
	// 将第二个参数转换为string类型
	msg := L.ToString(2)
	// 调用robotgo.ShowAlert函数，弹出一个提示框，返回值为bool类型
	abool := robotgo.ShowAlert(title, msg)
	// 将返回值转换为lua.LBool类型，并压入栈中
	L.Push(lua.LBool(abool))
	// 将弹出的Alert窗口置顶
	robotgo.ActiveName(title)
	// 返回参数个数1
	return 1
}

func myfunc(L *lua.LState) int {
	return 0
}
