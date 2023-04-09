package CallGo

import (
	KeyTool "GoRobotScript/GokeyLog/Tool"
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

var exports = map[string]lua.LGFunction{
	"myfunc": myfunc,
	"keyLog": keyLog,
}

func keyLog(L *lua.LState) int {
	KeyTool.KeyLog()
	return 0
}

func myfunc(L *lua.LState) int {
	return 0
}
