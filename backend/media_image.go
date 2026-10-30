package main

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	_ "image/png"
)

// 上传图片压缩：限制最大边 1600px，JPEG 重编码 q82，PNG 等比缩小后保持 PNG。
// WebP 标准库无编码器，原样保留。压缩后若反而变大则保留原图。
const maxImageDimension = 1600

func compressImage(content []byte, mime string) ([]byte, string) {
	img, _, err := image.Decode(bytes.NewReader(content))
	if err != nil {
		return content, mime
	}
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= maxImageDimension && h <= maxImageDimension && mime == "image/jpeg" {
		// 已经够小：仅重编码降体积
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 82}); err != nil {
			return content, mime
		}
		if buf.Len() < len(content) {
			return buf.Bytes(), mime
		}
		return content, mime
	}
	if mime == "image/webp" && w <= maxImageDimension && h <= maxImageDimension {
		return content, mime
	}
	scale := 1.0
	if w > maxImageDimension || h > maxImageDimension {
		if w > h {
			scale = float64(maxImageDimension) / float64(w)
		} else {
			scale = float64(maxImageDimension) / float64(h)
		}
	}
	nw, nh := int(float64(w)*scale+0.5), int(float64(h)*scale+0.5)
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	scaled := downscaleBox(img, bounds, nw, nh)
	var buf bytes.Buffer
	outMime := mime
	switch mime {
	case "image/png":
		if err := png.Encode(&buf, scaled); err != nil {
			return content, mime
		}
	default: // jpeg / webp(超大) 统一转 JPEG
		if err := jpeg.Encode(&buf, scaled, &jpeg.Options{Quality: 82}); err != nil {
			return content, mime
		}
		outMime = "image/jpeg"
	}
	if buf.Len() >= len(content) {
		return content, mime
	}
	return buf.Bytes(), outMime
}

// downscaleBox 用盒式采样缩小，质量介于 nearest 与 bilinear 之间，纯标准库。
func downscaleBox(src image.Image, bounds image.Rectangle, nw, nh int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	sw, sh := float64(bounds.Dx())/float64(nw), float64(bounds.Dy())/float64(nh)
	for y := 0; y < nh; y++ {
		for x := 0; x < nw; x++ {
			x0 := int(float64(x) * sw)
			x1 := int(float64(x+1)*sw + 0.5)
			y0 := int(float64(y) * sh)
			y1 := int(float64(y+1)*sh + 0.5)
			if x1 <= x0 {
				x1 = x0 + 1
			}
			if y1 <= y0 {
				y1 = y0 + 1
			}
			var r, g, b, a uint64
			var n uint64
			for sy := y0; sy < y1 && bounds.Min.Y+sy < bounds.Max.Y; sy++ {
				for sx := x0; sx < x1 && bounds.Min.X+sx < bounds.Max.X; sx++ {
					cr, cg, cb, ca := src.At(bounds.Min.X+sx, bounds.Min.Y+sy).RGBA()
					r += uint64(cr)
					g += uint64(cg)
					b += uint64(cb)
					a += uint64(ca)
					n++
				}
			}
			if n == 0 {
				continue
			}
			dst.SetRGBA(x, y, color.RGBA{
				R: uint8(r / n >> 8),
				G: uint8(g / n >> 8),
				B: uint8(b / n >> 8),
				A: uint8(a / n >> 8),
			})
		}
	}
	return dst
}
