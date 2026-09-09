package GoFSM

import (
	lua "github.com/yuin/gopher-lua"
)

// LuaLoader registers GoFSM into Lua runtime: local fsm = require("GoFSM")
func LuaLoader(L *lua.LState) int {
	mod := L.NewTable()

	L.SetField(mod, "new", L.NewFunction(func(L *lua.LState) int {
		m := NewMachine()
		ud := L.NewUserData()
		ud.Value = m
		L.SetMetatable(ud, L.GetTypeMetatable("GoFSM_Type"))
		L.Push(ud)
		return 1
	}))

	mt := L.NewTypeMetatable("GoFSM_Type")
	L.SetField(mt, "__index", L.SetFuncs(L.NewTable(), map[string]lua.LGFunction{
		"addState": func(L *lua.LState) int {
			ud := L.CheckUserData(1)
			m := ud.Value.(*Machine)
			name := L.CheckString(2)
			fn := L.CheckFunction(3)

			m.AddState(name, func(ctx *Context) (string, error) {
				subL := L
				subL.Push(fn)
				// push state name or context
				ctxTbl := subL.NewTable()
				for k, v := range ctx.Data {
					switch val := v.(type) {
					case string:
						subL.SetField(ctxTbl, k, lua.LString(val))
					case int:
						subL.SetField(ctxTbl, k, lua.LNumber(val))
					case float64:
						subL.SetField(ctxTbl, k, lua.LNumber(val))
					case bool:
						subL.SetField(ctxTbl, k, lua.LBool(val))
					}
				}
				subL.Push(ctxTbl)
				if err := subL.PCall(1, 1, nil); err != nil {
					return "", err
				}
				ret := subL.Get(-1)
				subL.Pop(1)
				if ret == lua.LNil {
					return "", nil
				}
				return ret.String(), nil
			})
			return 0
		},
		"setInitial": func(L *lua.LState) int {
			ud := L.CheckUserData(1)
			m := ud.Value.(*Machine)
			name := L.CheckString(2)
			m.SetInitialState(name)
			return 0
		},
		"step": func(L *lua.LState) int {
			ud := L.CheckUserData(1)
			m := ud.Value.(*Machine)
			next, err := m.Step()
			if err != nil {
				L.Push(lua.LNil)
				L.Push(lua.LString(err.Error()))
				return 2
			}
			L.Push(lua.LString(next))
			return 1
		},
		"getCurrent": func(L *lua.LState) int {
			ud := L.CheckUserData(1)
			m := ud.Value.(*Machine)
			L.Push(lua.LString(m.CurrentState()))
			return 1
		},
	}))

	L.Push(mod)
	return 1
}
