# GoRobotScript

## 使用方法

在编译后可以得到GokeyLog.exe、GokeyLua.exe和GokeyRun.exe。
GokeyLog.exe双击打开，会记录键盘和鼠标的操作；
GokeyLua.exe双击打开，默认调用同目录下的main.lua文件，或者cmd：GokeyLua.exe xxxx.lua执行lua脚本
GokeyRun.exe用法是将得到的.script文件拖动到GokeyRun.exe上，或者使用cmd：GokeyRun.exe xxxx.script执行脚本

## 目前问题

- Gokeylog记录的信息太多，特别是鼠标记录没有使用计时器进行控制，导致运行脚本时很可能执行的时间对不上，这也是后续需要改进的  

## Libraries for GoRobotScript

- [GitHub - yuin/gopher-lua: GopherLua: VM and compiler for Lua in Go](https://github.com/yuin/gopher-lua) : Call Package

- [GitHub - robotn/gohook: GoHook, Go global keyboard and mouse listener hook](https://github.com/robotn/gohook): Do thing  

## Donation

Null Now

## License

MIT

## Author

Suceru
