package v2render

import (
	"io"
	"os"
	"path/filepath"
	"photoeditor/backend/internal/v2input"
	"syscall"
)

// LoadPinnedAuthoredSegmentCalibrationCatalog reads a deployment-owned regular
// file through one descriptor. The independent deployment pin authenticates all
// envelope/profile bytes; special files, oversized input and concurrent changes
// are refused before the catalog can be used for preparation.
func LoadPinnedAuthoredSegmentCalibrationCatalog(path, expectedSHA string) (*PinnedAuthoredSegmentCalibrationCatalog, error) {
	if !filepath.IsAbs(path) {
		return nil, v2input.ErrInvalid
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, v2input.ErrInvalid
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 2 || info.Size() > 1048576 {
		return nil, v2input.ErrInvalid
	}
	raw, err := io.ReadAll(io.LimitReader(file, 1048577))
	if err != nil || int64(len(raw)) != info.Size() {
		return nil, v2input.ErrInvalid
	}
	after, err := file.Stat()
	if err != nil || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
		return nil, v2input.ErrInvalid
	}
	return NewPinnedAuthoredSegmentCalibrationCatalog(raw, expectedSHA)
}
