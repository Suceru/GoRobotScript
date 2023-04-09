package KeyTool

import (
	"os"            // 导入os包，提供了一个平台无关的操作系统API
	"path/filepath" // 导入filepath包，提供了一些操作文件路径的函数
)

// GetAppPath 函数返回当前应用程序的路径
// GetAppPath function returns the path of the current application
func GetAppPath() (string, error) {
	exePath, err := os.Getwd() // 获取当前工作目录 Get the current working directory
	if err != nil {
		return "", err // 如果获取失败，返回错误 Return error if failed to get
	}
	return filepath.Dir(exePath), nil // 返回当前应用程序的路径 Return the path of the current application
}

// GetScriptDir 函数返回当前脚本的路径
// GetScriptDir function returns the path of the current script
func GetScriptDir() (path string, err error) {
	scriptpath, err := GetAppPath() // 获取当前应用程序的路径 Get the path of the current application
	if err != nil {
		return "", err // 如果获取失败，返回错误 Return error if failed to get
	}
	scriptpath = filepath.Join(scriptpath, "script") // 将脚本文件夹路径与应用程序路径拼接 Join the script folder path with the application path
	_, err = os.Stat(scriptpath)                     // 获取脚本文件夹的信息 Get information about the script folder
	if os.IsNotExist(err) {                          // 如果脚本文件夹不存在 If the script folder does not exist
		err = os.MkdirAll(scriptpath, os.ModePerm) // 创建脚本文件夹 Create the script folder
		if err != nil {
			return "", err // 如果创建失败，返回错误 Return error if failed to create
		}
	}
	return scriptpath, nil // 返回脚本文件夹路径 Return the path of the script folder
}
