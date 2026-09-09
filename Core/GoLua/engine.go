// Package GoLua provides Lua runtime initialization, module registration, and asset-pak resolution.
package GoLua

import (
	"GoRobotScript/Core/GoFSM"
	"GoRobotScript/Core/GoInput"
	"GoRobotScript/Core/GoPak"
	"GoRobotScript/Core/GoVision"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-vgo/robotgo"
	lua "github.com/yuin/gopher-lua"
	"gocv.io/x/gocv"
)

// Environment holds the Lua state and script context.
type Environment struct {
	L        *lua.LState
	AppPath  string
	AssetDir string
}

// NewEnvironment creates a configured Lua environment with all Go core modules registered.
func NewEnvironment(baseDir string) *Environment {
	L := lua.NewState()

	if baseDir == "" {
		exe, err := os.Executable()
		if err == nil {
			baseDir = filepath.Dir(exe)
		} else {
			baseDir, _ = os.Getwd()
		}
	}

	assetDir := filepath.Join(baseDir, "Asset")

	env := &Environment{
		L:        L,
		AppPath:  baseDir,
		AssetDir: assetDir,
	}

	env.registerAllModules()
	return env
}

// Close closes the lua state.
func (env *Environment) Close() {
	if env.L != nil {
		env.L.Close()
	}
}

// LoadAssets extracts any existing .pak asset archive in baseDir into AssetDir,
// ensuring that resources under /Asset are ready for execution.
func (env *Environment) LoadAssets() error {
	// 1. Check if there is a .pak file in AppPath
	entries, err := os.ReadDir(env.AppPath)
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".pak") {
				pakPath := filepath.Join(env.AppPath, e.Name())
				// Extract into baseDir so that files under "Asset/..." extract into baseDir/Asset/...
				_, _ = GoPak.ExtractPak(pakPath, env.AppPath, false)
			}
		}
	}

	// 2. Ensure Asset directory exists
	_ = os.MkdirAll(env.AssetDir, 0755)
	return nil
}

// ExecuteString runs a lua script string.
func (env *Environment) ExecuteString(code string) error {
	cleanCode := strings.TrimPrefix(code, "\xef\xbb\xbf")
	return env.L.DoString(cleanCode)
}

// ExecuteFile runs a lua script file.
func (env *Environment) ExecuteFile(path string) error {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	cleanCode := strings.TrimPrefix(string(bytes), "\xef\xbb\xbf")
	return env.L.DoString(cleanCode)
}

func (env *Environment) registerAllModules() {
	L := env.L

	// 1. Core modules with "Go" prefix
	L.PreloadModule("GoVision", env.loadGoVision)
	L.PreloadModule("GoInput", env.loadGoInput)
	L.PreloadModule("GoFSM", GoFSM.LuaLoader)

	// 2. Backward compatibility aliases: SuScreen, SuKey, CallGo
	L.PreloadModule("SuScreen", env.loadGoVision)
	L.PreloadModule("SuKey", env.loadGoInput)
	L.PreloadModule("CallGo", env.loadCallGoCompat)

	// 3. Register global Asset helper & Package helpers
	L.SetGlobal("AssetPath", L.NewFunction(func(L *lua.LState) int {
		sub := L.OptString(1, "")
		p := filepath.Join(env.AssetDir, sub)
		L.Push(lua.LString(filepath.ToSlash(p)))
		return 1
	}))

	L.SetGlobal("PackageInfo", L.NewFunction(func(L *lua.LState) int {
		name := L.OptString(1, "App.exe")
		ver := L.OptString(2, "1.0.0")
		t := L.NewTable()
		t.RawSetString("name", lua.LString(name))
		t.RawSetString("version", lua.LString(ver))
		L.Push(t)
		return 1
	}))

	L.SetGlobal("AppInfo", L.NewFunction(func(L *lua.LState) int {
		t := L.NewTable()
		t.RawSetString("name", lua.LString(filepath.Base(env.AppPath)))
		t.RawSetString("version", lua.LString("1.0.0"))
		L.Push(t)
		return 1
	}))

	L.SetGlobal("PackageMode", L.NewFunction(func(L *lua.LState) int {
		return 0
	}))
}

func (env *Environment) loadGoVision(L *lua.LState) int {
	mod := L.NewTable()

	L.SetField(mod, "MatchTemplate", L.NewFunction(func(L *lua.LState) int {
		screenPath := L.CheckString(1)
		tplPath := L.CheckString(2)
		minScale := L.OptNumber(3, 0.5)
		maxScale := L.OptNumber(4, 1.5)
		step := L.OptNumber(5, 0.05)

		screen := gocv.IMRead(screenPath, gocv.IMReadColor)
		defer screen.Close()
		tpl := gocv.IMRead(tplPath, gocv.IMReadColor)
		defer tpl.Close()

		if screen.Empty() || tpl.Empty() {
			L.Push(lua.LNil)
			L.Push(lua.LString("failed to read screen or template image"))
			return 2
		}

		res, err := GoVision.MatchTemplate(screen, tpl, GoVision.MatchOptions{
			MinScale:  float64(minScale),
			MaxScale:  float64(maxScale),
			ScaleStep: float64(step),
		})
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}

		tbl := L.NewTable()
		tbl.RawSetString("x", lua.LNumber(res.X))
		tbl.RawSetString("y", lua.LNumber(res.Y))
		tbl.RawSetString("width", lua.LNumber(res.Width))
		tbl.RawSetString("height", lua.LNumber(res.Height))
		tbl.RawSetString("scale", lua.LNumber(res.Scale))
		tbl.RawSetString("score", lua.LNumber(res.Score))
		tbl.RawSetString("centerX", lua.LNumber(res.X+res.Width/2))
		tbl.RawSetString("centerY", lua.LNumber(res.Y+res.Height/2))

		L.Push(tbl)
		return 1
	}))

	L.SetField(mod, "MatchColorGrid", L.NewFunction(func(L *lua.LState) int {
		screenPath := L.CheckString(1)
		tplPath := L.CheckString(2)
		rows := L.OptInt(3, 3)
		cols := L.OptInt(4, 3)
		minScale := L.OptNumber(5, 0.5)
		maxScale := L.OptNumber(6, 1.5)
		step := L.OptNumber(7, 0.05)

		screen := gocv.IMRead(screenPath, gocv.IMReadColor)
		defer screen.Close()
		tpl := gocv.IMRead(tplPath, gocv.IMReadColor)
		defer tpl.Close()

		if screen.Empty() || tpl.Empty() {
			L.Push(lua.LNil)
			L.Push(lua.LString("failed to read screen or template image"))
			return 2
		}

		res, err := GoVision.ColorConvolutionMatch(screen, tpl, rows, cols, GoVision.MatchOptions{
			MinScale:  float64(minScale),
			MaxScale:  float64(maxScale),
			ScaleStep: float64(step),
		})
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}

		tbl := L.NewTable()
		tbl.RawSetString("x", lua.LNumber(res.X))
		tbl.RawSetString("y", lua.LNumber(res.Y))
		tbl.RawSetString("width", lua.LNumber(res.Width))
		tbl.RawSetString("height", lua.LNumber(res.Height))
		tbl.RawSetString("scale", lua.LNumber(res.Scale))
		tbl.RawSetString("score", lua.LNumber(res.Score))
		tbl.RawSetString("centerX", lua.LNumber(res.X+res.Width/2))
		tbl.RawSetString("centerY", lua.LNumber(res.Y+res.Height/2))

		L.Push(tbl)
		return 1
	}))

	L.SetField(mod, "GetPixel", L.NewFunction(func(L *lua.LState) int {
		screenPath := L.CheckString(1)
		x := L.CheckInt(2)
		y := L.CheckInt(3)
		c, err := GoVision.GetPixelRGB(screenPath, x, y)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}
		L.Push(lua.LNumber(c.R))
		L.Push(lua.LNumber(c.G))
		L.Push(lua.LNumber(c.B))
		return 3
	}))

	L.SetField(mod, "CheckMultiColor", L.NewFunction(func(L *lua.LState) int {
		screenPath := L.CheckString(1)
		tbl := L.CheckTable(2)
		tolerance := L.OptInt(3, 20)

		var pts []GoVision.CheckMultiColorPoint
		tbl.ForEach(func(k, v lua.LValue) {
			ptTbl, ok := v.(*lua.LTable)
			if ok {
				pts = append(pts, GoVision.CheckMultiColorPoint{
					X: int(L.GetField(ptTbl, "x").(lua.LNumber)),
					Y: int(L.GetField(ptTbl, "y").(lua.LNumber)),
					R: int(L.GetField(ptTbl, "r").(lua.LNumber)),
					G: int(L.GetField(ptTbl, "g").(lua.LNumber)),
					B: int(L.GetField(ptTbl, "b").(lua.LNumber)),
				})
			}
		})

		matched, ratio, err := GoVision.CheckMultiColor(screenPath, pts, tolerance)
		if err != nil {
			L.Push(lua.LBool(false))
			L.Push(lua.LString(err.Error()))
			return 2
		}
		L.Push(lua.LBool(matched))
		L.Push(lua.LNumber(ratio))
		return 2
	}))

	L.SetField(mod, "CaptureClient", L.NewFunction(func(L *lua.LState) int {
		title := L.CheckString(1)
		savePath := L.CheckString(2)

		hwnd := GoInput.FindWindow(title)
		if hwnd == 0 {
			L.Push(lua.LBool(false))
			L.Push(lua.LString("window not found"))
			return 2
		}

		x, y, w, h, ok := GoInput.GetClientBounds(hwnd)
		if !ok || w <= 0 || h <= 0 {
			L.Push(lua.LBool(false))
			L.Push(lua.LString("invalid window client bounds"))
			return 2
		}

		bmp := robotgo.CaptureScreen(x, y, w, h)
		_ = os.MkdirAll(filepath.Dir(savePath), 0755)
		err := robotgo.SavePng(robotgo.ToImage(bmp), savePath)
		if err != nil {
			L.Push(lua.LBool(false))
			L.Push(lua.LString(err.Error()))
			return 2
		}

		L.Push(lua.LBool(true))
		return 1
	}))

	L.SetField(mod, "CaptureScreen", L.NewFunction(func(L *lua.LState) int {
		name := L.ToString(1)
		tbl := L.ToTable(2)
		var arr []int
		tbl.ForEach(func(i lua.LValue, j lua.LValue) {
			arr = append(arr, int(j.(lua.LNumber)))
		})
		if len(arr) >= 4 {
			GoVision.CaptureScreen(name, arr[0], arr[1], arr[2], arr[3])
		}
		return 0
	}))

	L.SetField(mod, "SaveBitmap", L.NewFunction(func(L *lua.LState) int {
		name := L.ToString(1)
		dest := L.ToString(2)
		_ = GoVision.SaveBitmap(name, dest)
		return 0
	}))

	L.Push(mod)
	return 1
}

func (env *Environment) loadGoInput(L *lua.LState) int {
	mod := L.NewTable()

	L.SetField(mod, "KeyTap", L.NewFunction(func(L *lua.LState) int {
		arg1 := L.Get(1)
		if tbl, ok := arg1.(*lua.LTable); ok {
			var arr []string
			tbl.ForEach(func(k, v lua.LValue) {
				arr = append(arr, v.String())
			})
			if len(arr) > 1 {
				GoInput.KeyTap(arr[0], arr[1:]...)
			} else if len(arr) == 1 {
				GoInput.KeyTap(arr[0])
			}
			return 0
		}
		key := L.CheckString(1)
		var mods []string
		for i := 2; i <= L.GetTop(); i++ {
			mods = append(mods, L.ToString(i))
		}
		GoInput.KeyTap(key, mods...)
		return 0
	}))

	L.SetField(mod, "TypeStr", L.NewFunction(func(L *lua.LState) int {
		s := L.CheckString(1)
		GoInput.TypeStr(s)
		return 0
	}))

	L.SetField(mod, "Click", L.NewFunction(func(L *lua.LState) int {
		btn := L.OptString(1, "left")
		double := L.OptBool(2, false)
		GoInput.Click(btn, double)
		return 0
	}))

	L.SetField(mod, "Move", L.NewFunction(func(L *lua.LState) int {
		x := L.CheckInt(1)
		y := L.CheckInt(2)
		GoInput.Move(x, y)
		return 0
	}))

	L.SetField(mod, "Sleep", L.NewFunction(func(L *lua.LState) int {
		ms := L.CheckInt(1)
		GoInput.Sleep(ms)
		return 0
	}))

	L.SetField(mod, "ClickClient", L.NewFunction(func(L *lua.LState) int {
		title := L.CheckString(1)
		cx := L.CheckInt(2)
		cy := L.CheckInt(3)
		hwnd := GoInput.FindWindow(title)
		if hwnd == 0 {
			L.Push(lua.LBool(false))
			return 1
		}
		GoInput.PostClick(hwnd, cx, cy)
		L.Push(lua.LBool(true))
		return 1
	}))

	L.Push(mod)
	return 1
}

func (env *Environment) loadGoRecord(L *lua.LState) int {
	mod := L.NewTable()

	L.SetField(mod, "StartRecording", L.NewFunction(func(L *lua.LState) int {
		outPath := L.OptString(1, "")
		is3D := L.OptBool(2, true)
		rec, err := GoInput.StartSmartRecording(outPath, is3D)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}
		ud := L.NewUserData()
		ud.Value = rec
		L.Push(ud)
		return 1
	}))

	L.Push(mod)
	return 1
}

func (env *Environment) loadCallGoCompat(L *lua.LState) int {
	mod := L.NewTable()
	L.SetField(mod, "showalert", L.NewFunction(func(L *lua.LState) int {
		title := L.ToString(1)
		msg := L.ToString(2)
		res := GoInput.Alert(title, msg)
		L.Push(lua.LBool(res))
		return 1
	}))
	L.SetField(mod, "keyLog", L.NewFunction(func(L *lua.LState) int {
		_, _ = GoInput.StartSmartRecording("", true)
		return 0
	}))
	L.Push(mod)
	return 1
}
