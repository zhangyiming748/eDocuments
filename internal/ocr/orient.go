package ocr

import (
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"

	"github.com/disintegration/gift"
	"github.com/rwcarlsen/goexif/exif"
)

// autoOrient 按 JPEG EXIF 方向标签把图片转正。
// 非 JPEG、无 EXIF 或方向正常时直接返回原路径；需要旋转时写出临时 JPEG。
// 返回的 cleanup 用于删除临时文件。
func autoOrient(path string) (string, func(), error) {
	noop := func() {}
	if !strings.EqualFold(filepath.Ext(path), ".jpg") &&
		!strings.EqualFold(filepath.Ext(path), ".jpeg") {
		return path, noop, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return "", noop, err
	}
	orientation := 1
	if x, exifErr := exif.Decode(f); exifErr == nil {
		if tag, tagErr := x.Get(exif.Orientation); tagErr == nil {
			if v, intErr := tag.Int(0); intErr == nil && v >= 1 && v <= 8 {
				orientation = v
			}
		}
	}
	f.Close()

	if orientation == 1 {
		return path, noop, nil
	}

	// 重新打开供 image 解码（exif.Decode 已消费前一个 reader）。
	in, err := os.Open(path)
	if err != nil {
		return "", noop, err
	}
	img, _, err := image.Decode(in)
	in.Close()
	if err != nil {
		return "", noop, err
	}

	g := orientationFilter(orientation)
	dst := image.NewRGBA(g.Bounds(img.Bounds()))
	g.Draw(dst, img)

	tmp, err := os.CreateTemp("", "edocs-orient-*.jpg")
	if err != nil {
		return "", noop, err
	}
	if err := jpeg.Encode(tmp, dst, &jpeg.Options{Quality: 95}); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", noop, err
	}
	tmp.Close()
	return tmp.Name(), func() { _ = os.Remove(tmp.Name()) }, nil
}

// orientationFilter 把 EXIF 1～8 方向映射为 gift 滤镜组合。
func orientationFilter(o int) *gift.GIFT {
	switch o {
	case 2:
		return gift.New(gift.FlipHorizontal())
	case 3:
		return gift.New(gift.Rotate180())
	case 4:
		return gift.New(gift.FlipVertical())
	case 5: // 转置（沿主对角线镜像）
		return gift.New(gift.Rotate90(), gift.FlipHorizontal())
	case 6:
		return gift.New(gift.Rotate90())
	case 7: // 横转置（沿副对角线镜像）
		return gift.New(gift.Rotate90(), gift.FlipVertical())
	case 8:
		return gift.New(gift.Rotate270())
	default:
		return gift.New()
	}
}
