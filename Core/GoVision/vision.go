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

// MatchOptions controls the multi-scale and position search.
type MatchOptions struct {
	MinScale     float64
	MaxScale     float64
	ScaleStep    float64
	PositionStep int
}

// MatchResult describes the result of template or color grid matching.
type MatchResult struct {
	X, Y          int
	Width, Height int
	Scale         float64
	Score         float64
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
	if screen.Empty() || template.Empty() {
		return MatchResult{}, errors.New("screen and template must be non-empty")
	}
	if screen.Channels() != template.Channels() {
		return MatchResult{}, errors.New("screen and template must have the same channel count")
	}
	o := options.normalized()
	best := MatchResult{Score: math.Inf(1)}
	for scale := o.MinScale; scale <= o.MaxScale+o.ScaleStep/2; scale += o.ScaleStep {
		w, h := scaledSize(template.Cols(), template.Rows(), scale)
		if w > screen.Cols() || h > screen.Rows() {
			continue
		}
		resized := gocv.NewMat()
		if err := gocv.Resize(template, &resized, image.Pt(w, h), 0, 0, gocv.InterpolationArea); err != nil {
			resized.Close()
			return MatchResult{}, err
		}
		result := gocv.NewMat()
		mask := gocv.NewMat()
		err := gocv.MatchTemplate(screen, resized, &result, gocv.TmSqdiffNormed, mask)
		mask.Close()
		resized.Close()
		if err != nil {
			result.Close()
			return MatchResult{}, err
		}
		minVal, minLoc := minFloatLoc(result, o.PositionStep)
		result.Close()
		if float64(minVal) < best.Score {
			best = MatchResult{X: minLoc.X, Y: minLoc.Y, Width: w, Height: h, Scale: scale, Score: float64(minVal)}
		}
	}
	if math.IsInf(best.Score, 1) {
		return MatchResult{}, errors.New("template is larger than screen at every requested scale")
	}
	return best, nil
}

// ColorConvolutionMatch pools the template to a rows x cols grid and matches.
func ColorConvolutionMatch(screen, template gocv.Mat, rows, cols int, options MatchOptions) (MatchResult, error) {
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
	best := MatchResult{Score: math.Inf(1)}
	for scale := o.MinScale; scale <= o.MaxScale+o.ScaleStep/2; scale += o.ScaleStep {
		w, h := scaledSize(template.Cols(), template.Rows(), scale)
		if w > screen.Cols() || h > screen.Rows() {
			continue
		}
		grid := gocv.NewMat()
		if err := gocv.Resize(template, &grid, image.Pt(cols, rows), 0, 0, gocv.InterpolationArea); err != nil {
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
		err := gocv.MatchTemplate(screen, descriptor, &result, gocv.TmSqdiffNormed, mask)
		mask.Close()
		descriptor.Close()
		if err != nil {
			result.Close()
			return MatchResult{}, err
		}
		minVal, minLoc := minFloatLoc(result, o.PositionStep)
		result.Close()
		if float64(minVal) < best.Score {
			best = MatchResult{X: minLoc.X, Y: minLoc.Y, Width: w, Height: h, Scale: scale, Score: float64(minVal)}
		}
	}
	if math.IsInf(best.Score, 1) {
		return MatchResult{}, errors.New("template is larger than screen at every requested scale")
	}
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

func minFloatLoc(mat gocv.Mat, step int) (float32, image.Point) {
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
