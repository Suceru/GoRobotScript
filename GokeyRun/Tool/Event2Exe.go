package KeyTool

import (
	"fmt"
	"time"

	"github.com/go-vgo/robotgo"

	hook "github.com/robotn/gohook"
)

var Scripttime time.Time
var ScreenRect [4]float32 = [4]float32{0, 0, 1534.0 / 1920, 862.0 / 1080}

func Scriptexe(e *hook.Event) string {
	if Scripttime.IsZero() {
		Scripttime = e.When
		//x, y := robotgo.GetMousePos()
		robotgo.Move(0, 0)
		return fmt.Sprintf("Event: {Kind: Start}")
	} else {
		Delay := e.When.Sub(Scripttime)
		Scripttime = Scripttime.Add(Delay)
		switch e.Kind {
		case hook.HookEnabled:
			robotgo.MilliSleep(int(Delay.Milliseconds()))
			return fmt.Sprintf("Event: {Kind: HookEnabled}")
		case hook.HookDisabled:
			robotgo.MilliSleep(int(Delay.Milliseconds()))
			return fmt.Sprintf("Event: {Kind: HookDisabled}")
		case hook.KeyUp:
			robotgo.MilliSleep(int(Delay.Milliseconds()))
			robotgo.KeyToggle(string(hook.RawcodetoKeychar(e.Rawcode)), "up")
			return fmt.Sprintf("Event: {Kind: KeyUp, Rawcode: %v, Keychar: %v}", e.Rawcode, e.Keychar)
		case hook.KeyHold:
			robotgo.MilliSleep(int(Delay.Milliseconds()))
			return fmt.Sprintf("Event: {Kind: KeyHold, Rawcode: %v, Keychar: %v}", e.Rawcode, e.Keychar)
		case hook.KeyDown:
			robotgo.MilliSleep(int(Delay.Milliseconds()))
			robotgo.KeyToggle(string(e.Keychar), "down")
			return fmt.Sprintf("Event: {Kind: KeyDown, Rawcode: %v, Keychar: %v}", e.Rawcode, e.Keychar)
		case hook.MouseUp:
			robotgo.MilliSleep(int(Delay.Milliseconds()))
			robotgo.MouseToggle("up", string(e.Button))
			return fmt.Sprintf("Event: {Kind: MouseUp, Button: %v, X: %v, Y: %v, Clicks: %v}", e.Button, e.X, e.Y, e.Clicks)
		case hook.MouseHold:
			robotgo.MilliSleep(int(Delay.Milliseconds()))
			return fmt.Sprintf("Event: {Kind: MouseHold, Button: %v, X: %v, Y: %v, Clicks: %v}", e.Button, e.X, e.Y, e.Clicks)
		case hook.MouseDown:
			robotgo.MilliSleep(int(Delay.Milliseconds()))
			robotgo.MouseToggle("down", string(e.Button))
			return fmt.Sprintf("Event: {Kind: MouseDown, Button: %v, X: %v, Y: %v, Clicks: %v}", e.Button, e.X, e.Y, e.Clicks)
		case hook.MouseMove:

			//robotgo.MoveMouseSmooth(int(e.X), int(e.Y), 8.0, 20.0, int(Delay.Milliseconds()))
			robotgo.MoveMouseSmooth(int(float32(e.X)*ScreenRect[2]), int(float32(e.Y)*ScreenRect[3]), 0.01, 0.005, int(Delay.Milliseconds()))
			x, y := robotgo.GetMousePos()
			return fmt.Sprintf("Event: {Kind: MouseMove, Button: %v, X: %v->%v, Y: %v->%v, Clicks: %v}", e.Button, x, e.X, y, e.Y, e.Clicks)
		case hook.MouseDrag:
			robotgo.DragSmooth(int(float32(e.X)*ScreenRect[2]), int(float32(e.Y)*ScreenRect[3]), 8.0, 20.0, int(Delay.Milliseconds()))
			x, y := robotgo.GetMousePos()
			return fmt.Sprintf("Event: {Kind: MouseDrag,  Button: %v, X: %v->%v, Y: %v->%v, Clicks: %v}", e.Button, x, e.X, y, e.Y, e.Clicks)
		case hook.MouseWheel:
			robotgo.MilliSleep(int(Delay.Milliseconds()))
			return fmt.Sprintf("Event: {Kind: MouseWheel, Amount: %v, Rotation: %v, Direction: %v}", e.Amount, e.Rotation, e.Direction)
		case hook.FakeEvent:
			robotgo.MilliSleep(int(Delay.Milliseconds()))
			return fmt.Sprintf("Event: {Kind: FakeEvent}")
		}

	}

	return "Unknown event, contact the mantainers."
}
