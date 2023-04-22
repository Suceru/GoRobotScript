package main

import (
	CallGo "GoRobotScript/GokeyLua/Tool/CallGo"
	SuKey "GoRobotScript/GokeyLua/Tool/SuKey"
	"flag"
	"fmt"
	lua "github.com/yuin/gopher-lua"
	"os"
)

func ReadscriptUsage() {
	HaveusageText := `ScriptPath is err`
	fmt.Fprintf(os.Stderr, "%s\n\n", HaveusageText)

}
func main() {

	L := lua.NewState()
	defer L.Close()
	L.PreloadModule("CallGo", CallGo.Loader)
	L.PreloadModule("SuKey", SuKey.Loader)
	scriptpath := flag.String("script", "path", "Script Path")
	//scriptpatht := *scriptpath
	Simkeyrun := "main"
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
		if err := L.DoFile("main.lua"); err != nil {
			panic(err)
		}
	default:
		if *scriptpath == "path" {
			if err := L.DoFile(os.Args[1]); err != nil {
				panic(err)
			}
		} else {
			fmt.Fprintf(os.Stderr, "value=%s", *scriptpath)

		}
	}

}
