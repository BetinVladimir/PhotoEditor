package masknpy

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
)

// RenderProbabilityMaskPreview verifies the complete pinned NPY before it
// returns a bounded transparent red PNG. Each output pixel is the mean source
// probability in its cell; no unverified or partially read image is exposed.
func RenderProbabilityMaskPreview(ctx context.Context, reader io.Reader, size int64, sha string, width, height int64, maxSide int) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if maxSide < 64 || maxSide > 1024 || width < 1 || height < 1 || width > 200000000 || height > 200000000/width {
		return nil, ErrInvalid
	}
	previewW, previewH := width, height
	if width >= height && width > int64(maxSide) {
		previewW = int64(maxSide)
		previewH = (height*previewW + width/2) / width
	} else if height > width && height > int64(maxSide) {
		previewH = int64(maxSide)
		previewW = (width*previewH + height/2) / height
	}
	if previewW < 1 {
		previewW = 1
	}
	if previewH < 1 {
		previewH = 1
	}
	count := int(previewW * previewH)
	sums, samples := make([]float64, count), make([]uint32, count)
	err := verifyArrayNPY(ctx, reader, size, sha, width, height, 0, true, false, false, false, func(index int64, probability float32) bool {
		x, y := index%width, index/width
		cell := int((y*previewH/height)*previewW + x*previewW/width)
		sums[cell] += float64(probability)
		samples[cell]++
		return true
	}, false)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	canvas := image.NewNRGBA(image.Rect(0, 0, int(previewW), int(previewH)))
	for i := 0; i < count; i++ {
		if samples[i] == 0 {
			return nil, ErrInvalid
		}
		alpha := uint8(math.RoundToEven(220 * sums[i] / float64(samples[i])))
		canvas.SetNRGBA(i%int(previewW), i/int(previewW), color.NRGBA{R: 255, G: 32, B: 48, A: alpha})
	}
	var output bytes.Buffer
	if err := png.Encode(&output, canvas); err != nil {
		return nil, err
	}
	return output.Bytes(), ctx.Err()
}
