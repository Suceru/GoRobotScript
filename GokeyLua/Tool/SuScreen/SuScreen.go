package SuScreen

import (
	KeyTool "GoRobotScript/GokeyLog/Tool"
	"fmt"
	"github.com/go-vgo/robotgo"      // 导入robotgo库
	lua "github.com/yuin/gopher-lua" // 导入gopher-lua库
	"os"
	"path/filepath"
	"strconv" // 导入strconv库
	"strings" // 导入strings库
)

var SuBitmap = make(map[string]robotgo.CBitmap) /*创建集合 */

func Loader(L *lua.LState) int {
	// register functions to the table
	mod := L.SetFuncs(L.NewTable(), exports) // 将函数注册到table中
	// register other stuff
	L.SetField(mod, "name", lua.LString("value")) // 将name字段设置为value

	// returns the module
	L.Push(mod) // 将mod压入栈中
	return 1    // 返回1
}

// 定义一个map类型的变量exports，其中key为string类型，value为lua.LGFunction类型
var exports = map[string]lua.LGFunction{
	"CaptureScreen": captureScreen, // 将captureScreen函数注册到exports中
	"SaveBitmap":    saveBitmap,    // 将saveBitmap函数注册到exports中
	"ReadBitmap":    readBitmap,
	"ReadAllBitmap": readAllBitmap,
}

func readAllBitmap(L *lua.LState) int {
	path := L.ToString(1)
	var files []string
	err := filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			switch filepath.Ext(path) {
			case ".png", ".jpg", "":
				files = append(files, strings.TrimSuffix(path, filepath.Ext(path)))
			}
		}
		return nil
	})
	if err != nil {
		panic(err)
	}
	for _, file := range files {
		GreadBitmap(strings.ReplaceAll(file, "\\", "/")) // 把添加到files的反斜杠路径改为正斜杠路径
	}
	return 0
}

// captureScreen函数接收一个LState类型的参数L，返回一个int类型的值
// 从L中获取第一个参数，转换为table类型
func captureScreen(L *lua.LState) int {
	name := L.ToString(1) // 获取第一个参数并转换为string类型
	tbl := L.ToTable(2)   // 获取第二个参数并转换为table类型
	var arr []int         // 定义一个int类型的数组
	tbl.ForEach(func(i lua.LValue, j lua.LValue) {
		// 将table中的值转换为int类型
		if num, err := strconv.Atoi(j.String()); err == nil {
			arr = append(arr, num) // 将转换后的值添加到数组中
		}
	})
	//bit := robotgo.GoCaptureScreen(arr[0], arr[1], arr[2], arr[3])
	SuBitmap[name] = robotgo.CaptureScreen(arr[0], arr[1], arr[2], arr[3]) // 将截图保存到SuBitmap中
	L.SetGlobal("SuBitmap", L.NewUserData())                               // 将SuBitmap设置为全局变量
	return 0                                                               // 返回0
}

// saveBitmap函数接收一个LState类型的参数L，返回一个int类型的值
func saveBitmap(L *lua.LState) int {
	haven := false
	name := L.ToString(1) // 获取第一个参数并转换为string类型
	_, ok := SuBitmap[name]
	if ok {
		haven = true
		//fmt.Println(GetDir(name))
		path, _ := GetDir(name)
		filename, _ := GetFileName(name)
		if L.GetTop() == 1 {

			robotgo.Save(robotgo.ToImage(SuBitmap[name]), filepath.Join(path, filename)) // 保存截图
			return 0                                                                     // 返回0
		}
		end := strings.ToLower(L.ToString(2))
		switch end {
		case ".png":
			robotgo.SavePng(robotgo.ToImage(SuBitmap[name]), filepath.Join(path, filename)+".png") // 保存为png格式
		case ".jpg":
			robotgo.SaveJpeg(robotgo.ToImage(SuBitmap[name]), filepath.Join(path, filename)+".jpg") // 保存为jpg格式
		default:
			robotgo.Save(robotgo.ToImage(SuBitmap[name]), filepath.Join(path, filename)+end) // 保存为默认格式
		}
	}

	L.Push(lua.LBool(haven))
	return 1 // 返回1
}
func readBitmap(L *lua.LState) int {
	name := L.ToString(1) // 获取第一个参数并转换为string
	L.Push(lua.LBool(GreadBitmap(name)))
	return 1 // 返回1
}
func GreadBitmap(name string) bool {
	haven := false
	dir, _ := GetDir("")
	end := []string{".png", ".jpg", ""}
	var file1 = ""
	for _, s := range end {
		file1 = filepath.Join(dir, name+s)
		if _, err := os.Stat(file1); err == nil {
			file1 = filepath.Join(dir, name+s)
			break
		}
	}

	println(file1)
	img, err := robotgo.Read(file1)
	if err != nil {
		fmt.Println("Error: ", err.Error()+" "+name) // 提示错误信息
	} else {
		SuBitmap[name] = robotgo.ImgToCBitmap(img)
		haven = true
	}
	return haven // 返回1
}

func GetDir(name string) (path string, err error) {
	scriptpath, err := KeyTool.GetAppPath() // 获取当前应用程序的路径 Get the path of the current application
	if err != nil {
		return "", err // 如果获取失败，返回错误 Return error if failed to get
	}
	// 获取文件的父路径
	dir := filepath.Dir(name)
	path = filepath.Join(scriptpath, dir) // 将应用程序路径与相对路径拼接 Join the application path with the relative path
	_, err = os.Stat(path)                // 获取脚本文件夹的信息 Get information about the script folder
	if os.IsNotExist(err) {               // 如果脚本文件夹不存在 If the script folder does not exist
		err = os.MkdirAll(path, os.ModePerm) // 创建脚本文件夹 Create the script folder
		if err != nil {
			return "", err // 如果创建失败，返回错误 Return error if failed to create
		}
	}
	return path, nil // 返回脚本文件夹路径 Return the path of the script folder
}
func GetFileName(name string) (path string, err error) {
	_, fileName := filepath.Split(name) // 获取文件名
	return fileName, nil                // 返回文件名
}

// GetFileEnd GetFileEnd函数接收一个string类型的参数name，返回一个string类型的值
// 仅获取文件拓展名
func GetFileEnd(name string) (path string, err error) {
	_, fileName := filepath.Split(name) // 获取文件名
	return filepath.Ext(fileName), nil  // 返回文件拓展名
}
