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

- [GoCV](https://gocv.io/): OpenCV-backed multi-scale template and color-grid matching.

## Vision scanner

`vision_utils.go` uses `gocv.io/x/gocv` and exposes two matchers:

- `MatchTemplate`: resizes an arbitrary template across `MinScale..MaxScale` and uses normalized squared difference. A 256x256 template can therefore match a 128x128 target with scale `0.5`.
- `ColorConvolutionMatch`: area-resizes the template and each candidate region to any grid, such as 3x3 for nine colors, then compares the pooled BGR values.

The desktop capture/identification script is `cmd/vision_scan`:

```powershell
go run .\cmd\vision_scan -capture .\screen.png
go run .\cmd\vision_scan -capture .\screen.png -activate -enter-map
```

The second command activates a window whose title contains `Survivalcraft`, opens `PLAY`, selects the requested world row, and starts it through client-area window messages. It never sends input when no matching window is found.

GoCV requires OpenCV C++ headers and DLLs. For Windows, install the versions documented by GoCV (currently OpenCV 4.13 for GoCV 0.43), ensure MinGW is on `PATH`, and then run the commands above. The repository already pins `gocv.io/x/gocv v0.43.0`.

## Donation

Null Now

## License

MIT

## Author

Suceru
