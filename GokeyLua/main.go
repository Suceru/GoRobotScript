package main

import (
	CallGo "GoRobotScript/GokeyLua/Tool"
	lua "github.com/yuin/gopher-lua"
)

func main() {
	L := lua.NewState()
	defer L.Close()
	L.PreloadModule("CallGo", CallGo.Loader)
	if err := L.DoFile("GokeyLua/hello.lua"); err != nil {
		panic(err)
	}
}
