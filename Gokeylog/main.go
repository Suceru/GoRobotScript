package main

import (
	"Gokeylog/Tool"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	// "github.com/go-vgo/robotgo"

	hook "github.com/robotn/gohook"
)

func main() {
	add()
	low()
	//event()
}

func add() {
	fmt.Println("--- Please press ctrl + shift + q to stop hook ---")
	hook.Register(hook.KeyDown, []string{"["}, func(e hook.Event) {
		fmt.Println("[")
		hook.End()
	})
	s := hook.Start()
	<-hook.Process(s)

}

func low() {

	evChan := hook.Start()
	defer hook.End()
	scriptpath, _ := KeyTool.GetScriptDir()
	scriptpath = filepath.Join(scriptpath, time.Now().Format("2006-01-02-150405")+".script")
	file, err := os.OpenFile(scriptpath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer file.Close()
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

func event() {
	ok := hook.AddEvents("q", "ctrl", "shift")
	if ok {
		fmt.Println("add events...")
	}

	keve := hook.AddEvent("k")
	if keve {
		fmt.Println("you press... ", "k")
	}

	mleft := hook.AddEvent("mleft")
	if mleft {
		fmt.Println("you press... ", "mouse left button")
	}
}
