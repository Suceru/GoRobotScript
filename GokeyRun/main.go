package main

import (
	KeyTool "GoRobotScript/GokeyRun/Tool"
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	hook "github.com/robotn/gohook"
)

func main() {
	scriptpath := flag.String("script", "path", "Script Path")
	//scriptpatht := *scriptpath
	Simkeyrun := "a"
	flag.Parse()
	if len(os.Args) == 1 {
		flag.Usage = ReadscriptUsage
		flag.Usage()
		fmt.Fprintf(os.Stderr, "errargs")
		return
	}
	switch os.Args[1] {
	// case "dokey":

	// 	s := Keyboard.String("cmd", "", "传入参数")
	// 	Keyboard.Parse(os.Args[2:])
	// 	for index, val := range *s {

	// 		fmt.Println(index, string(val))
	// 	}
	// 	FKeyboard(*s)
	case Simkeyrun:
		fmt.Println("cmd=" + Simkeyrun)
	default:
		if *scriptpath == "path" {
			Readscriptrun(os.Args[1])
		} else {
			fmt.Fprintf(os.Stderr, "value=%s", *scriptpath)

		}
	}
}

func ReadscriptUsage() {
	HaveusageText := `ScriptPath is err`
	fmt.Fprintf(os.Stderr, "%s\n\n", HaveusageText)

}

func SplitLines(s string) (lines []hook.Event) {
	var value hook.Event
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {

		err := json.Unmarshal(sc.Bytes(), &value)
		if err != nil {
			fmt.Println(err)
		}

		//value.When=time.Time(arr[0])
		lines = append(lines, value)
	}
	return lines
}

var quit bool = false

func Readscriptrun(pathstr string) {
	_, err := os.Stat(pathstr)
	if os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Script is not find")
		os.Exit(0)
	}
	content, err := os.ReadFile(pathstr)
	if err != nil {
		panic(err)
	}
	scriptevent := SplitLines(string(content))
	add()
	for index, value := range scriptevent {
		if quit {
			fmt.Println("quit")
			os.Exit(0)
		}
		fmt.Println(strconv.Itoa(index) + ":" + KeyTool.Scriptexe(&value))
		fmt.Println(strconv.Itoa(index) + KeyTool.Scripttime.Local().String())
		//fmt.Println(strconv.Itoa(index) + "+++ " + value.String())
	}

}
func add() {
	fmt.Println("--- Please press ctrl + shift + q to stop hook ---")
	hook.Register(hook.KeyDown, []string{"["}, func(e hook.Event) {
		fmt.Println("[")
		hook.End()
	})
	hook.Register(hook.KeyDown, []string{"]"}, func(e hook.Event) {
		quit = !quit
		fmt.Println("]")
	})
	s := hook.Start()
	<-hook.Process(s)

}
