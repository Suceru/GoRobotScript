package SuKey

import (
	"fmt"
	"github.com/go-vgo/robotgo"
	"github.com/yuin/gopher-lua"
)

func Loader(L *lua.LState) int {
	// register functions to the table
	mod := L.SetFuncs(L.NewTable(), exports)
	// register other stuff
	L.SetField(mod, "name", lua.LString("value"))

	// returns the module
	L.Push(mod)
	return 1
}

// 定义一个map类型的变量exports，其中key为string类型，value为lua.LGFunction类型
var exports = map[string]lua.LGFunction{
	"TypeStr": typeStr, // 将myfunc函数注册到exports中
	"KeyTap":  keyTap,
}

// keyTap函数接收一个LState类型的参数L，返回一个int类型的值
func keyTap(L *lua.LState) int {
	// 从L中获取第一个参数，转换为table类型
	tbl := L.ToTable(1)
	// 将table转换为字符串数组
	var arr []string
	tbl.ForEach(func(i lua.LValue, j lua.LValue) {
		arr = append(arr, j.String())
	})

	// 打印字符串数组
	fmt.Println(arr)
	// 如果字符串数组长度大于1，则调用robotgo包中的KeyTap函数，传入第一个元素和剩余元素组成的切片
	if len(arr) > 1 {
		robotgo.KeyTap(arr[0], arr[1:])
	} else {
		// 否则，只传入第一个元素
		robotgo.KeyTap(arr[0])
	}
	// 返回0
	return 0
}

func typeStr(L *lua.LState) int {
	str := L.ToString(1)
	robotgo.TypeStr(str)
	return 0
}

/*self := L.CheckTable(1)
value := lua.LNumber(0)
// 第二个为可选参数
if L.GetTop() >= 2 {
value = L.CheckNumber(2)
}
current := L.GetField(self, "value").(lua.LNumber)*/
