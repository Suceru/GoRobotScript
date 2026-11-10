package GoVision

import (
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"runtime"

	"github.com/go-vgo/robotgo"
	"gocv.io/x/gocv"
)

// ScreenSize 返回主显示器分辨率 (宽, 高)
func ScreenSize() (int, int) {
	return robotgo.GetScreenSize()
}

// ClipToScreen 把矩形裁剪到屏幕范围内；完全在屏幕外时返回空矩形。
func ClipToScreen(x, y, w, h, screenW, screenH int) image.Rectangle {
	if w <= 0 || h <= 0 {
		return image.Rectangle{}
	}
	return image.Rect(x, y, x+w, y+h).Intersect(image.Rect(0, 0, screenW, screenH))
}

// PatchGeometry 计算以 (cx, cy) 光标为中心、边长 size 的正方形采样区域。
//
// **样本图尺寸恒定**为 size×size（便于后续统一处理、统一网格描述与匹配）：
// 光标贴近屏幕边缘时**不把区域裁小**，而是把整块区域向屏幕内平移，
// 因此返回的 (iox, ioy) 会偏离 size/2 —— 它始终是光标在样本图内的真实偏移，
// 回放时用 `命中点 + iox*scale` 即可还原光标坐标。
//
// 只有屏幕本身比采样块还小时才退化为整屏（此时尺寸无法保证）。
func PatchGeometry(cx, cy, size int) (rect image.Rectangle, iox, ioy int, ok bool) {
	if size <= 0 {
		return image.Rectangle{}, 0, 0, false
	}
	sw, sh := ScreenSize()
	if sw <= 0 || sh <= 0 {
		return image.Rectangle{}, 0, 0, false
	}
	// 光标必须落在屏幕内，否则无法确定采样偏移
	if cx < 0 || cy < 0 || cx >= sw || cy >= sh {
		return image.Rectangle{}, 0, 0, false
	}

	w, h := size, size
	if w > sw {
		w = sw
	}
	if h > sh {
		h = sh
	}

	x0 := cx - w/2
	if x0 > sw-w {
		x0 = sw - w
	}
	if x0 < 0 {
		x0 = 0
	}
	y0 := cy - h/2
	if y0 > sh-h {
		y0 = sh - h
	}
	if y0 < 0 {
		y0 = 0
	}
	return image.Rect(x0, y0, x0+w, y0+h), cx - x0, cy - y0, true
}

// CaptureRegionMat 抓取屏幕矩形区域，返回 BGR 三通道 Mat。
func CaptureRegionMat(rect image.Rectangle) (gocv.Mat, error) {
	if rect.Dx() <= 0 || rect.Dy() <= 0 {
		return gocv.NewMat(), errors.New("capture region is empty")
	}
	// GDI 抓屏需要固定的 OS 线程语义，避免协程迁移导致的偶发失败
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	bmp := robotgo.CaptureScreen(rect.Min.X, rect.Min.Y, rect.Dx(), rect.Dy())
	defer robotgo.FreeBitmap(bmp)

	img := robotgo.ToImage(bmp)
	if img == nil {
		return gocv.NewMat(), errors.New("capture screen returned empty image")
	}
	mat, err := gocv.ImageToMatRGB(img)
	if err != nil {
		return gocv.NewMat(), err
	}
	return mat, nil
}

// CaptureFullScreenMat 抓取整个主显示器画面 (回放识图定位用)。
func CaptureFullScreenMat() (gocv.Mat, error) {
	sw, sh := ScreenSize()
	return CaptureRegionMat(image.Rect(0, 0, sw, sh))
}

// SaveRegionPNG 抓取屏幕区域并保存为 PNG，父目录自动创建。
func SaveRegionPNG(rect image.Rectangle, destPath string) error {
	mat, err := CaptureRegionMat(rect)
	if err != nil {
		return err
	}
	defer mat.Close()

	if dir := filepath.Dir(destPath); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	if !gocv.IMWrite(destPath, mat) {
		return fmt.Errorf("cannot write image: %s", destPath)
	}
	return nil
}

// CapturePatchPNG 以 (cx, cy) 为中心采样 size×size 的画面并保存为 PNG。
// 返回光标在图片内的相对偏移 (图片被屏幕边界裁剪时偏移会随之变化)。
func CapturePatchPNG(cx, cy, size int, destPath string) (iox, ioy int, err error) {
	rect, iox, ioy, ok := PatchGeometry(cx, cy, size)
	if !ok {
		return 0, 0, errors.New("patch geometry out of screen bounds")
	}
	if err := SaveRegionPNG(rect, destPath); err != nil {
		return 0, 0, err
	}
	return iox, ioy, nil
}

// LoadTemplate 读取模板图片为 BGR Mat。
func LoadTemplate(filePath string) (gocv.Mat, error) {
	img := gocv.IMRead(filePath, gocv.IMReadColor)
	if img.Empty() {
		img.Close()
		return gocv.NewMat(), errors.New("cannot load image: " + filePath)
	}
	return img, nil
}
