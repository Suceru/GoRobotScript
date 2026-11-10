package GoVision

import (
	"errors"
	"image"

	"gocv.io/x/gocv"
)

// ---------------------------------------------------------------------------
// ROI 限定匹配 与 多图匹配
//
// 这两项能力用于"快速校验"：已知某个区域大概在哪时，只在它周围的一小块 ROI 里
// 重新匹配，成本比全屏搜索低 1~2 个数量级；同时对**多张图**分别匹配，
// 全部命中且相对几何一致才认为校验通过，否则回退到全屏正常识别。
// ---------------------------------------------------------------------------

// ClampROI 把 ROI 裁剪到画面范围内
func ClampROI(roi image.Rectangle, screen gocv.Mat) image.Rectangle {
	if screen.Empty() {
		return image.Rectangle{}
	}
	bounds := image.Rect(0, 0, screen.Cols(), screen.Rows())
	r := roi.Intersect(bounds)
	if r.Dx() <= 0 || r.Dy() <= 0 {
		return image.Rectangle{}
	}
	return r
}

// MatchTemplateROI 在指定 ROI 内做多尺度模板匹配，返回**绝对**坐标结果。
// ROI 为空或越界时会被裁剪；裁剪后放不下模板则返回错误（调用方据此回退全屏识别）。
func MatchTemplateROI(screen, template gocv.Mat, roi image.Rectangle, options MatchOptions) (MatchResult, error) {
	r := ClampROI(roi, screen)
	if r.Dx() <= 0 || r.Dy() <= 0 {
		return MatchResult{}, errors.New("roi is empty after clamping")
	}
	sub := screen.Region(r)
	defer sub.Close()
	return matchTemplate(sub, template, options, image.Pt(r.Min.X, r.Min.Y))
}

// ColorConvolutionMatchROI 在指定 ROI 内做颜色卷积匹配，返回**绝对**坐标结果。
func ColorConvolutionMatchROI(screen, template gocv.Mat, roi image.Rectangle, rows, cols int, options MatchOptions) (MatchResult, error) {
	r := ClampROI(roi, screen)
	if r.Dx() <= 0 || r.Dy() <= 0 {
		return MatchResult{}, errors.New("roi is empty after clamping")
	}
	sub := screen.Region(r)
	defer sub.Close()
	return colorConvolutionMatch(sub, template, rows, cols, options, image.Pt(r.Min.X, r.Min.Y))
}

// MultiMatchItem 一次多图匹配中的一项
type MultiMatchItem struct {
	Template gocv.Mat
	ROI      image.Rectangle // 零值 = 整幅画面
	Color    bool            // true: 颜色卷积匹配；false: 模板匹配
	GridRows int             // 颜色卷积的行数 (Color=true 时有效，<=0 取 3)
	GridCols int             // 颜色卷积的列数
	Options  MatchOptions
}

// MultiMatchResult 多图匹配中单项的结果
type MultiMatchResult struct {
	Index int
	MatchResult
	Err error
}

// MatchMulti 在同一幅画面上对多张模板分别匹配（各自独立 ROI），全部返回**绝对**坐标。
// 单项失败只影响该项 (Err 非空)，不会中断整批。
func MatchMulti(screen gocv.Mat, items []MultiMatchItem) []MultiMatchResult {
	out := make([]MultiMatchResult, len(items))
	for i, it := range items {
		out[i].Index = i
		var (
			res MatchResult
			err error
		)
		if it.Color {
			rows, cols := it.GridRows, it.GridCols
			if rows <= 0 {
				rows = 3
			}
			if cols <= 0 {
				cols = 3
			}
			res, err = ColorConvolutionMatchROI(screen, it.Template, it.ROI, rows, cols, it.Options)
		} else {
			res, err = MatchTemplateROI(screen, it.Template, it.ROI, it.Options)
		}
		out[i].MatchResult = res
		out[i].Err = err
	}
	return out
}

// CenterROI 由"目标左上角坐标 + 模板尺寸"推出搜索 ROI：
// 以该位置为中心、四周各外扩 radius 像素（再按画面裁剪）。
func CenterROI(left, top, width, height, radius int, screen gocv.Mat) image.Rectangle {
	if screen.Empty() {
		return image.Rectangle{}
	}
	return ClampROI(image.Rect(
		left-radius, top-radius,
		left+width+radius, top+height+radius,
	), screen)
}
