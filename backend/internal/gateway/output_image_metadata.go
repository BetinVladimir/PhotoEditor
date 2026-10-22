package gateway

import (
	"bytes"
	"golang.org/x/image/webp"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"photoeditor/backend/internal/objectstore"
)

// Intrinsic output dimensions come only from bytes consumed by the immutable
// writer. Keep a bounded encoded header, never a decoded full-resolution image.
const imageMetadataHeaderLimit = 8 << 20
const imageMetadataMaxPixels int64 = 200000000

type intrinsicImageMetadata struct{ Width, Height int }
type imageMetadataPrefix struct{ data []byte }

func (p *imageMetadataPrefix) Write(b []byte) (int, error) {
	n := len(b)
	if remaining := imageMetadataHeaderLimit - len(p.data); remaining > 0 {
		if len(b) > remaining {
			b = b[:remaining]
		}
		p.data = append(p.data, b...)
	}
	return n, nil
}
func imageMetadataRequired(media string) bool {
	return media == "image/jpeg" || media == "image/png" || media == "image/webp"
}
func decodeIntrinsicImageMetadata(media string, header []byte) (*intrinsicImageMetadata, error) {
	var c image.Config
	var err error
	r := bytes.NewReader(header)
	switch media {
	case "image/jpeg":
		c, err = jpeg.DecodeConfig(r)
	case "image/png":
		c, err = png.DecodeConfig(r)
	case "image/webp":
		c, err = webp.DecodeConfig(r)
	default:
		return nil, objectstore.ErrInvalid
	}
	if err != nil || c.Width < 1 || c.Height < 1 || c.Width > 200000 || c.Height > 200000 || int64(c.Width) > imageMetadataMaxPixels/int64(c.Height) {
		return nil, objectstore.ErrIntegrity
	}
	return &intrinsicImageMetadata{c.Width, c.Height}, nil
}
func captureIntrinsicImageMetadata(media string, body io.Reader) (io.Reader, *imageMetadataPrefix) {
	if !imageMetadataRequired(media) {
		return body, nil
	}
	p := &imageMetadataPrefix{}
	return io.TeeReader(body, p), p
}
