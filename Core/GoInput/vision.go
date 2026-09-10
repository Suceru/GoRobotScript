package GoInput

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"GoRobotScript/Core/GoVision"
)

// ---------------------------------------------------------------------------
// 识图 (Vision) 采样：录制期在鼠标按下/松开处截取模板图，回放期用该模板
// 在屏幕上重新定位关键点，并对关键点之间的路径做旋转/拉伸对齐。
// ---------------------------------------------------------------------------

// VisionPreset 识图预设槽
type VisionPreset struct {
	Slot int    // 槽位号 (1 ~ VisionPresetCount)
	Size int    // 采样边长 (像素)
	Mode string // "image" 图像模板匹配 / "color" 颜色卷积匹配
}

// 匹配方式常量
const (
	VisionModeImage = "image"
	VisionModeColor = "color"
)

// VisionPresetCount 预设槽总数
const VisionPresetCount = 6

// VisionPresets 6 个固定预设槽，按匹配方式分组：先 图像(32/64/128)，再 颜色(32/64/128)。
// 预设只存在于单轮录制内，PgDn 结束录制后即丢弃，不跨轮保留。
var VisionPresets = [VisionPresetCount]VisionPreset{
	{Slot: 1, Size: 32, Mode: VisionModeImage},
	{Slot: 2, Size: 64, Mode: VisionModeImage},
	{Slot: 3, Size: 128, Mode: VisionModeImage},
	{Slot: 4, Size: 32, Mode: VisionModeColor},
	{Slot: 5, Size: 64, Mode: VisionModeColor},
	{Slot: 6, Size: 128, Mode: VisionModeColor},
}

// VisionPresetAt 取槽位 (1 基)，越界时归一到槽位 1
func VisionPresetAt(slot int) VisionPreset {
	if slot < 1 || slot > VisionPresetCount {
		slot = 1
	}
	return VisionPresets[slot-1]
}

// VisionModeName 匹配方式的中文名
func VisionModeName(mode string) string {
	if mode == VisionModeColor {
		return "颜色"
	}
	return "图像"
}

// VisionStatusText 固定格式的识图状态文本。
// 例：识图关闭 / 识图开启-图像32*32p / 识图开启-颜色128*128p
func VisionStatusText(on bool, slot int) string {
	if !on {
		return "识图关闭"
	}
	p := VisionPresetAt(slot)
	return fmt.Sprintf("识图开启-%s%d*%dp", VisionModeName(p.Mode), p.Size, p.Size)
}

// VisionAssetDir 识图样本目录约定：与脚本同级、同名的 <脚本名>.vision 目录。
// 例如 bin/script/A.script 的样本目录为 bin/script/A.vision/
func VisionAssetDir(scriptPath string) string {
	base := filepath.Base(scriptPath)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	return filepath.Join(filepath.Dir(scriptPath), base+".vision")
}

// visionStatusFieldWidth 状态行固定显示宽度 (全角按 2 列计)，
// 保证每次刷新都覆盖同一行同一列，不撑长控制台。
const visionStatusFieldWidth = 78

// visionDisplayWidth 估算字符串的控制台显示宽度 (CJK 等宽字符按 2 列)
func visionDisplayWidth(s string) int {
	w := 0
	for _, r := range s {
		if r > 0x2000 {
			w += 2
		} else {
			w++
		}
	}
	return w
}

// visionCaptureJob 一次异步采样任务
type visionCaptureJob struct {
	name string
	rect image.Rectangle
	dir  string
}

// visionInit 初始化识图子系统 (由 StartSmartRecording 调用)
func (r *StreamlineRecorder) visionInit(scriptPath string) {
	r.visionSlot = 1
	r.visionSeq = make(map[string]int)
	r.visionDir = VisionAssetDir(scriptPath)
	r.visionQueue = make(chan visionCaptureJob, 512)
	go r.visionCaptureWorker()

	// 绘图框：显示当前这一枪会截取的范围，长按 Home 开关
	r.visionBox = newVisionBox(func() int {
		return VisionPresetAt(r.VisionSlot()).Size
	})
}

// visionCaptureWorker 独立线程异步落盘，绝不阻塞 60FPS 录制主循环
func (r *StreamlineRecorder) visionCaptureWorker() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	for job := range r.visionQueue {
		dest := filepath.Join(job.dir, job.name+".png")
		if err := GoVision.SaveRegionPNG(job.rect, dest); err != nil {
			fmt.Printf("\r[Vision] 采样保存失败 %s: %v\n", job.name, err)
		}
	}
}

// VisionEnabled 当前是否开启识图
func (r *StreamlineRecorder) VisionEnabled() bool {
	r.visionMu.Lock()
	defer r.visionMu.Unlock()
	return r.visionOn
}

// VisionSlot 当前槽位号
func (r *StreamlineRecorder) VisionSlot() int {
	r.visionMu.Lock()
	defer r.visionMu.Unlock()
	return r.visionSlot
}

// ToggleVision 切换识图开关 (Pause 键)，返回切换后的开启状态
func (r *StreamlineRecorder) ToggleVision() bool {
	r.visionMu.Lock()
	r.visionOn = !r.visionOn
	on := r.visionOn
	dir := r.visionDir
	r.visionMu.Unlock()

	if on && dir != "" {
		_ = os.MkdirAll(dir, 0755)
	}
	if !on {
		// 识图关闭 => 绘图框一并收起 (绘图框只在识图开启时可用)
		r.SetVisionBox(false)
	}
	return on
}

// SetVisionBox 开关光标绘图框，返回开启后的状态
func (r *StreamlineRecorder) SetVisionBox(on bool) bool {
	if r.visionBox == nil {
		return false
	}
	if on {
		// 只在识图开启时允许显示
		if !r.VisionEnabled() {
			return false
		}
		r.visionBox.start()
	}
	r.visionBox.setEnabled(on)
	return on
}

// ToggleVisionBox 长按 Home 时切换绘图框，返回开启后的状态
func (r *StreamlineRecorder) ToggleVisionBox() bool {
	if r.visionBox == nil || !r.VisionEnabled() {
		return false
	}
	return r.SetVisionBox(!r.visionBox.isEnabled())
}

// VisionBoxEnabled 绘图框当前是否显示
func (r *StreamlineRecorder) VisionBoxEnabled() bool {
	if r.visionBox == nil {
		return false
	}
	return r.visionBox.isEnabled()
}

// ShiftVisionSlot 切换预设槽 (Home 前进 +1 / End 后退 -1，环形)，返回新槽位
func (r *StreamlineRecorder) ShiftVisionSlot(delta int) VisionPreset {
	r.visionMu.Lock()
	r.visionSlot += delta
	for r.visionSlot < 1 {
		r.visionSlot += VisionPresetCount
	}
	for r.visionSlot > VisionPresetCount {
		r.visionSlot -= VisionPresetCount
	}
	p := VisionPresets[r.visionSlot-1]
	r.visionMu.Unlock()
	return p
}

// DisableVision 结束录制时退出识图 (预设不保留到下一轮)，返回此前是否开启
func (r *StreamlineRecorder) DisableVision() bool {
	r.visionMu.Lock()
	was := r.visionOn
	r.visionOn = false
	r.visionSlot = 1
	r.visionMu.Unlock()

	r.visionCloseQueue()
	r.visionBox.stop() // 绘图框一并销毁，不在下一轮残留
	return was
}

// visionCloseQueue 幂等关闭采样队列
func (r *StreamlineRecorder) visionCloseQueue() {
	r.visionMu.Lock()
	if !r.visionQueueClosed && r.visionQueue != nil {
		r.visionQueueClosed = true
		close(r.visionQueue)
	}
	r.visionMu.Unlock()
}

// PrintVisionStatus 以固定列宽覆盖式刷新识图状态行 (不换行、不撑长控制台)
func (r *StreamlineRecorder) PrintVisionStatus() {
	on := r.VisionEnabled()
	slot := r.VisionSlot()
	line := "[Vision] " + VisionStatusText(on, slot)
	if on {
		box := "关"
		if r.VisionBoxEnabled() {
			box = "开"
		}
		line += fmt.Sprintf("  槽位%d/%d  绘图框:%s  (短按Home切槽/长按开关框)", slot, VisionPresetCount, box)
	}
	pad := visionStatusFieldWidth - visionDisplayWidth(line)
	if pad < 0 {
		pad = 0
	}
	fmt.Printf("\r%s%s", line, strings.Repeat(" ", pad))
}

// visionTakeSample 在鼠标事件处采样：返回样本名 (不含扩展名) 与光标在样本图内的偏移。
// ok=false 表示当前未开启识图，调用方不应写入样本字段。
func (r *StreamlineRecorder) visionTakeSample(op, btn string, x, y int) (name string, iox, ioy, size int, mode string, ok bool) {
	r.visionMu.Lock()
	if !r.visionOn {
		r.visionMu.Unlock()
		return "", 0, 0, 0, "", false
	}
	p := VisionPresets[r.visionSlot-1]
	key := op + "-" + btn
	r.visionSeq[key]++
	seq := r.visionSeq[key]
	dir := r.visionDir
	closed := r.visionQueueClosed
	queue := r.visionQueue
	r.visionMu.Unlock()

	// 序号 3 位：md-left-001 / mu-right-012 ...
	name = fmt.Sprintf("%s-%03d", key, seq)
	rect, oix, oiy, geoOK := GoVision.PatchGeometry(x, y, p.Size)
	if !geoOK {
		return "", 0, 0, 0, "", false
	}
	r.enqueueVisionCapture(queue, closed, dir, name, rect)
	return name, oix, oiy, p.Size, p.Mode, true
}

func (r *StreamlineRecorder) enqueueVisionCapture(queue chan visionCaptureJob, closed bool, dir, name string, rect image.Rectangle) {
	if closed || queue == nil {
		return
	}
	select {
	case queue <- visionCaptureJob{name: name, rect: rect, dir: dir}:
	default:
		fmt.Printf("\r[Vision] 采样队列已满，丢弃 %s\n", name)
	}
}
