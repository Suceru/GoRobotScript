// Package GoInput provides mouse, keyboard, and window interaction primitives.
package GoInput

import (
	"time"

	"github.com/go-vgo/robotgo"
)

// KeyTap presses and releases a key or key combinations (e.g. KeyTap("a"), KeyTap("c", "ctrl")).
func KeyTap(key string, modifiers ...string) {
	if len(modifiers) > 0 {
		var args []interface{}
		for _, m := range modifiers {
			args = append(args, m)
		}
		robotgo.KeyTap(key, args...)
	} else {
		robotgo.KeyTap(key)
	}
}

// KeyToggle presses down or releases a key ("down" or "up").
func KeyToggle(key string, direction string) {
	robotgo.KeyToggle(key, direction)
}

// TypeStr types a string of characters.
func TypeStr(s string) {
	robotgo.TypeStr(s)
}

// Move moves the mouse instantly to (x, y).
func Move(x, y int) {
	robotgo.Move(x, y)
}

// MoveSmooth moves the mouse smoothly to (x, y) with low latency.
func MoveSmooth(x, y int, low, high float64, delayMs int) {
	robotgo.MoveMouseSmooth(x, y, low, high, delayMs)
}

// Click clicks mouse button ("left", "right", "center"), optional double click.
func Click(button string, double bool) {
	robotgo.Click(button, double)
}

// MouseDown presses mouse button down.
func MouseDown(button string) {
	robotgo.MouseDown(button)
}

// MouseUp releases mouse button.
func MouseUp(button string) {
	robotgo.MouseUp(button)
}

// GetMousePos returns current cursor coordinates.
func GetMousePos() (int, int) {
	return robotgo.GetMousePos()
}

// Sleep halts current goroutine for ms milliseconds.
func Sleep(ms int) {
	time.Sleep(time.Duration(ms) * time.Millisecond)
}

// Alert displays a system message dialog.
func Alert(title, msg string) bool {
	res := robotgo.Alert(title, msg)
	robotgo.ActiveName(title)
	return res
}
