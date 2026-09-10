// Package GoVision provides high-performance, reusable OpenCV computer vision and screen analysis primitives.
package GoVision

import (
	"errors"
	"image"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-vgo/robotgo"
	"gocv.io/x/gocv"
)

// MatchMetric 匹配指标
type MatchMetric int

const (
	// MetricSqDiffNormed 归一化平方差：0 最佳（越小越像）。
	// 对亮度/对比度变化免疫，但对"整体差一点"的模糊/锐化很敏感。
	MetricSqDiffNormed MatchMetric = iota
	// MetricCCoeffNormed 零均值归一化互相关 (ZNCC)：1 最佳（越大越像）。
	// 先减去各自均值再算相关，因此对整体亮度、对比度、模糊/锐化造成的
	// 逐像素差异都不敏感 —— 看的是**结构/特征**而不是像素值，
	// 并且天然给出 0~1 的"匹配度"，可直接换算成百分比。
	MetricCCoeffNormed
)

// MatchOptions controls the multi-scale and position search.
type MatchOptions struct {
	MinScale     float64
	MaxScale     float64
	ScaleStep    float64
	PositionStep int
	Metric       MatchMetric // 默认 MetricSqDiffNormed（保持既有行为）
	Blur         int         // 匹配前对模板与画面各做一次 Blur×Blur 高斯模糊（自动取奇数，<=1 不模糊）
}

// MatchResult describes the result of a template or color grid matching.
type MatchResult struct {
	X, Y          int
	Width, Height int
	Scale         float64
	Score         float64 // 原始指标值（含义随 MatchOptions.Metric 而定）
	Percent       float64 // 匹配度百分比 0~100（与指标无关，越大越像）
}

// SimilarityPercent 把原始指标换算成 0~100 的匹配度百分比
func SimilarityPercent(score float64, metric MatchMetric) float64 {
	if metric == MetricCCoeffNormed {
		return clamp01(score) * 100
	}
	// 平方差指标的理论范围是 [0,4]（0 完全一致，4 完全反相）。
	// 这里做一个单调映射，仅用于展示；判断是否命中仍建议用原始指标。
	return clamp01(1-score/2) * 100
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// gocvMethod 对应的 OpenCV 匹配方法
func (o MatchOptions) gocvMethod() gocv.TemplateMatchMode {
	if o.Metric == MetricCCoeffNormed {
		return gocv.TmCcoeffNormed
	}
	return gocv.TmSqdiffNormed
}

// better 判断 a 是否优于 b
func (o MatchOptions) better(a, b float64) bool {
	if o.Metric == MetricCCoeffNormed {
		return a > b
	}
	return a < b
}

// worst 最差分数的初值
func (o MatchOptions) worst() float64 {
	if o.Metric == MetricCCoeffNormed {
		return math.Inf(-1)
	}
	return math.Inf(1)
}

// prepare 按需做统一预模糊：动态模糊、局部锐化、分辨率降低都只会改变高频细节，
// 先把两边都糊一下再比，匹配看的就是结构/特征，而不是逐个像素。
func (o MatchOptions) prepare(screen, template gocv.Mat) (gocv.Mat, gocv.Mat, func(), error) {
	k := o.Blur
	if k%2 == 0 {
		k++
	}
	if k <= 1 {
		return screen, template, func() {}, nil
	}
	bs := gocv.NewMat()
	bt := gocv.NewMat()
	if err := gocv.GaussianBlur(screen, &bs, image.Pt(k, k), 0, 0, gocv.BorderDefault); err != nil {
		bs.Close()
		bt.Close()
		return screen, template, func() {}, err
	}
	if err := gocv.GaussianBlur(template, &bt, image.Pt(k, k), 0, 0, gocv.BorderDefault); err != nil {
		bs.Close()
		bt.Close()
		return screen, template, func() {}, err
	}
	return bs, bt, func() { bs.Close(); bt.Close() }, nil
}

// bestLoc 在结果矩阵里取最佳位置（平方差取最小，互相关取最大）
func bestLoc(mat gocv.Mat, o MatchOptions, step int) (float64, image.Point) {
	if step <= 1 && !mat.Empty() {
		mn, mx, mnLoc, mxLoc := gocv.MinMaxLoc(mat)
		if o.Metric == MetricCCoeffNormed {
			return float64(mx), mxLoc
		}
		return float64(mn), mnLoc
	}
	best := o.worst()
	loc := image.Point{}
	for y := 0; y < mat.Rows(); y += step {
		for x := 0; x < mat.Cols(); x += step {
			if v := float64(mat.GetFloatAt(y, x)); o.better(v, best) {
				best, loc = v, image.Pt(x, y)
			}
		}
	}
	return best, loc
}

func (o MatchOptions) normalized() MatchOptions {
	if o.MinScale <= 0 {
		o.MinScale = 0.25
	}
	if o.MaxScale <= 0 {
		o.MaxScale = 2
	}
	if o.MaxScale < o.MinScale {
		o.MinScale, o.MaxScale = o.MaxScale, o.MinScale
	}
	if o.ScaleStep <= 0 {
		o.ScaleStep = 0.05
	}
	if o.PositionStep <= 0 {
		o.PositionStep = 1
	}
	return o
}

// MatchTemplate performs multi-scale normalized squared-difference template matching.
func MatchTemplate(screen, template gocv.Mat, options MatchOptions) (MatchResult, error) {
	return matchTemplate(screen, template, options, image.Point{})
}

// matchTemplate 内部实现，origin 用于把结果换算回绝对坐标
func matchTemplate(screen, template gocv.Mat, options MatchOptions, origin image.Point) (MatchResult, error) {
	if screen.Empty() || template.Empty() {
		return MatchResult{}, errors.New("screen and template must be non-empty")
	}
	if screen.Channels() != template.Channels() {
		return MatchResult{}, errors.New("screen and template must have the same channel count")
	}
	o := options.normalized()
	src, tpl, release, err := o.prepare(screen, template)
	if err != nil {
		return MatchResult{}, err
	}
	defer release()

	method := o.gocvMethod()
	best := MatchResult{Score: o.worst()}
	for scale := o.MinScale; scale <= o.MaxScale+o.ScaleStep/2; scale += o.ScaleStep {
		w, h := scaledSize(tpl.Cols(), tpl.Rows(), scale)
		if w > src.Cols() || h > src.Rows() {
			continue
		}
		resized := gocv.NewMat()
		if err := gocv.Resize(tpl, &resized, image.Pt(w, h), 0, 0, gocv.InterpolationArea); err != nil {
			resized.Close()
			return MatchResult{}, err
		}
		result := gocv.NewMat()
		mask := gocv.NewMat()
		err := gocv.MatchTemplate(src, resized, &result, method, mask)
		mask.Close()
		resized.Close()
		if err != nil {
			result.Close()
			return MatchResult{}, err
		}
		val, loc := bestLoc(result, o, o.PositionStep)
		result.Close()
		if o.better(val, best.Score) {
			best = MatchResult{X: loc.X, Y: loc.Y, Width: w, Height: h, Scale: scale, Score: val}
		}
	}
	if math.IsInf(best.Score, 1) || math.IsInf(best.Score, -1) {
		return MatchResult{}, errors.New("template is larger than screen at every requested scale")
	}
	best.Percent = SimilarityPercent(best.Score, o.Metric)
	best.X += origin.X
	best.Y += origin.Y
	return best, nil
}

// ColorConvolutionMatch pools the template to a rows x cols grid and matches.
func ColorConvolutionMatch(screen, template gocv.Mat, rows, cols int, options MatchOptions) (MatchResult, error) {
	return colorConvolutionMatch(screen, template, rows, cols, options, image.Point{})
}

// colorConvolutionMatch 内部实现，origin 用于把结果换算回绝对坐标
func colorConvolutionMatch(screen, template gocv.Mat, rows, cols int, options MatchOptions, origin image.Point) (MatchResult, error) {
	if screen.Empty() || template.Empty() {
		return MatchResult{}, errors.New("screen and template must be non-empty")
	}
	if screen.Channels() != 3 || template.Channels() != 3 {
		return MatchResult{}, errors.New("color matching requires 3-channel BGR images")
	}
	if rows <= 0 || cols <= 0 {
		return MatchResult{}, errors.New("grid rows and columns must be positive")
	}
	o := options.normalized()
	src, tpl, release, err := o.prepare(screen, template)
	if err != nil {
		return MatchResult{}, err
	}
	defer release()

	method := o.gocvMethod()
	best := MatchResult{Score: o.worst()}
	for scale := o.MinScale; scale <= o.MaxScale+o.ScaleStep/2; scale += o.ScaleStep {
		w, h := scaledSize(tpl.Cols(), tpl.Rows(), scale)
		if w > src.Cols() || h > src.Rows() {
			continue
		}
		grid := gocv.NewMat()
		if err := gocv.Resize(tpl, &grid, image.Pt(cols, rows), 0, 0, gocv.InterpolationArea); err != nil {
			grid.Close()
			return MatchResult{}, err
		}
		descriptor := gocv.NewMat()
		if err := gocv.Resize(grid, &descriptor, image.Pt(w, h), 0, 0, gocv.InterpolationNearestNeighbor); err != nil {
			grid.Close()
			descriptor.Close()
			return MatchResult{}, err
		}
		grid.Close()
		result := gocv.NewMat()
		mask := gocv.NewMat()
		err := gocv.MatchTemplate(src, descriptor, &result, method, mask)
		mask.Close()
		descriptor.Close()
		if err != nil {
			result.Close()
			return MatchResult{}, err
		}
		val, loc := bestLoc(result, o, o.PositionStep)
		result.Close()
		if o.better(val, best.Score) {
			best = MatchResult{X: loc.X, Y: loc.Y, Width: w, Height: h, Scale: scale, Score: val}
		}
	}
	if math.IsInf(best.Score, 1) || math.IsInf(best.Score, -1) {
		return MatchResult{}, errors.New("template is larger than screen at every requested scale")
	}
	best.Percent = SimilarityPercent(best.Score, o.Metric)
	best.X += origin.X
	best.Y += origin.Y
	return best, nil
}

// PixelColor contains RGB channels.
type PixelColor struct {
	R, G, B int
}

// GetPixelRGB reads pixel RGB at coordinate (x, y) from image at filePath.
func GetPixelRGB(filePath string, x, y int) (PixelColor, error) {
	img := gocv.IMRead(filePath, gocv.IMReadColor)
	defer img.Close()
	if img.Empty() {
		return PixelColor{}, errors.New("cannot load image: " + filePath)
	}
	if x < 0 || x >= img.Cols() || y < 0 || y >= img.Rows() {
		return PixelColor{}, errors.New("coordinates out of bounds")
	}
	vec := img.GetVecbAt(y, x) // BGR
	return PixelColor{R: int(vec[2]), G: int(vec[1]), B: int(vec[0])}, nil
}

// CheckMultiColorPoint represents expected point RGB color.
type CheckMultiColorPoint struct {
	X, Y    int
	R, G, B int
}

// CheckMultiColor checks a list of points against image color with tolerance.
func CheckMultiColor(filePath string, points []CheckMultiColorPoint, tolerance int) (bool, float64, error) {
	img := gocv.IMRead(filePath, gocv.IMReadColor)
	defer img.Close()
	if img.Empty() {
		return false, 0, errors.New("cannot load image: " + filePath)
	}
	if len(points) == 0 {
		return true, 1.0, nil
	}
	matched := 0
	total := 0
	for _, pt := range points {
		if pt.X >= 0 && pt.X < img.Cols() && pt.Y >= 0 && pt.Y < img.Rows() {
			total++
			vec := img.GetVecbAt(pt.Y, pt.X)
			b, g, r := int(vec[0]), int(vec[1]), int(vec[2])
			if absInt(r-pt.R) <= tolerance && absInt(g-pt.G) <= tolerance && absInt(b-pt.B) <= tolerance {
				matched++
			}
		}
	}
	if total == 0 {
		return false, 0, errors.New("no valid points within image bounds")
	}
	ratio := float64(matched) / float64(total)
	return matched == total, ratio, nil
}

// BitmapCache global map for legacy / fast bitmap manipulation.
var BitmapCache = make(map[string]robotgo.CBitmap)

// CaptureScreen captures an area into BitmapCache under a key.
func CaptureScreen(key string, x, y, w, h int) {
	BitmapCache[key] = robotgo.CaptureScreen(x, y, w, h)
}

// SaveBitmap saves a cached bitmap to disk.
func SaveBitmap(key string, destPath string) error {
	bmp, ok := BitmapCache[key]
	if !ok {
		return errors.New("bitmap not found: " + key)
	}
	dir := filepath.Dir(destPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	ext := strings.ToLower(filepath.Ext(destPath))
	switch ext {
	case ".png":
		return robotgo.SavePng(robotgo.ToImage(bmp), destPath)
	case ".jpg", ".jpeg":
		return robotgo.SaveJpeg(robotgo.ToImage(bmp), destPath)
	default:
		return robotgo.Save(robotgo.ToImage(bmp), destPath)
	}
}

func scaledSize(w, h int, scale float64) (int, int) {
	return maxInt(1, int(math.Round(float64(w)*scale))), maxInt(1, int(math.Round(float64(h)*scale)))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// minFloatLoc 在匹配结果矩阵里找最小值位置。
//
// 关键性能点：结果矩阵尺寸约为 (画面宽-模板宽)×(画面高-模板高)，在 2560x1440 上
// 是 300 万级别。若用 Go 侧逐像素 GetFloatAt 遍历，每次都是一次 cgo 调用，
// 单次全屏匹配会退化到**秒级**；交给 OpenCV 原生 MinMaxLoc 只需**毫秒级**。
// 只有需要"按步长粗扫"(step > 1) 时才回退到手动遍历。
func minFloatLoc(mat gocv.Mat, step int) (float32, image.Point) {
	if step <= 1 && !mat.Empty() {
		minVal, _, minLoc, _ := gocv.MinMaxLoc(mat)
		return minVal, minLoc
	}
	best := float32(math.Inf(1))
	loc := image.Point{}
	for y := 0; y < mat.Rows(); y += step {
		for x := 0; x < mat.Cols(); x += step {
			if v := mat.GetFloatAt(y, x); v < best {
				best, loc = v, image.Pt(x, y)
			}
		}
	}
	return best, loc
}
