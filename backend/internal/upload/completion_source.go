package upload

import (
	"path/filepath"
	"strings"
	"unicode/utf8"

	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/rawdevelopment"
)

func validCompletionSource(lease CompletionLease) bool {
	if lease.ExpectedBytes < 1 || lease.ExpectedBytes > objectstore.MaxUploadBytes ||
		lease.Filename == "" || lease.Filename != strings.TrimSpace(lease.Filename) ||
		!utf8.ValidString(lease.Filename) || len([]rune(lease.Filename)) > 255 ||
		strings.ContainsAny(lease.Filename, "\x00/\\") || !rawdevelopment.Valid(lease.RAWDevelopment) {
		return false
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(lease.Filename)), ".")
	media, ok := formats[ext]
	if !ok || media != lease.MediaType {
		return false
	}
	if lease.RAWDevelopment != nil &&
		(ext == "jpg" || ext == "jpeg" || ext == "png" || ext == "tif" || ext == "tiff") {
		return false
	}
	if lease.AssumeProfile != nil {
		switch *lease.AssumeProfile {
		case "srgb_v1", "linear_srgb_v1", "work_rec2020_gamma24_v1":
		default:
			return false
		}
	}
	if lease.ExpectedSHA != nil {
		if len(*lease.ExpectedSHA) != 64 {
			return false
		}
		for _, char := range *lease.ExpectedSHA {
			if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
				return false
			}
		}
	}
	return true
}
