package main

import (
	KeyTool "GoRobotScript/GokeyLog/Tool"
	"fmt"
	// "github.com/go-vgo/robotgo"

	hook "github.com/robotn/gohook"
)

func main() {
	add()
	KeyTool.KeyLog()
	//event()
}

func add() {
	fmt.Println("--- Please press [ to start hook ---")
	hook.Register(hook.KeyDown, []string{"["}, func(e hook.Event) {
		fmt.Println("[")
		hook.End()
	})
	s := hook.Start()
	<-hook.Process(s)

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
