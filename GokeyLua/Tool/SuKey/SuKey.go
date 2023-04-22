package SuKey

import (
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
	"TypeStr": typeStr, // 将myfunc函数注册到exports中

}

func typeStr(L *lua.LState) int {
	str := L.ToString(1)
	robotgo.TypeStr(str)
	return 0
}
