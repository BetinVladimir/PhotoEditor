package workflow

import (
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const (
	MaxDownloadItems       = 5000
	DefaultVolumeByteLimit = int64(10 * 1024 * 1024 * 1024)
	maxEntryNameBytes      = 180
)

type DownloadSource struct {
	Ordinal        int64     `json:"ordinal"`
	AssetVersionID uuid.UUID `json:"asset_version_id"`
	ArtifactID     uuid.UUID `json:"artifact_id"`
	SHA256         string    `json:"sha256"`
	Bytes          int64     `json:"bytes"`
	Filename       string    `json:"filename"`
}

type PlannedDownload struct {
	DownloadSource
	EntryName    string `json:"entry_name"`
	Volume       int    `json:"volume_no"`
	DeliveryKind string `json:"delivery_kind"`
}

// PlanDownloadVolumes preserves snapshot order and deterministically assigns safe
// flat names. Oversized inputs remain explicit single-file deliveries.
func PlanDownloadVolumes(items []DownloadSource, byteLimit int64) ([]PlannedDownload, error) {
	if len(items) < 1 || len(items) > MaxDownloadItems || byteLimit < 1 || byteLimit > DefaultVolumeByteLimit {
		return nil, errors.New("invalid download plan bounds")
	}
	out := make([]PlannedDownload, 0, len(items))
	used := map[string]bool{}
	volume, files, bytes := 1, 0, int64(0)
	for index, item := range items {
		digest, digestErr := hex.DecodeString(item.SHA256)
		if item.Ordinal != int64(index) || item.AssetVersionID == uuid.Nil || item.ArtifactID == uuid.Nil ||
			item.Bytes < 1 || digestErr != nil || len(digest) != 32 {
			return nil, errors.New("invalid ordered download source")
		}
		name := uniqueDownloadName(item.Filename, item.SHA256, used)
		if item.Bytes > byteLimit {
			if files > 0 {
				volume++
				files, bytes = 0, 0
			}
			out = append(out, PlannedDownload{DownloadSource: item, EntryName: name, Volume: volume, DeliveryKind: "single"})
			volume++
			continue
		}
		if files > 0 && (files == MaxDownloadItems || bytes+item.Bytes > byteLimit) {
			volume++
			files, bytes = 0, 0
		}
		out = append(out, PlannedDownload{DownloadSource: item, EntryName: name, Volume: volume, DeliveryKind: "zip"})
		files++
		bytes += item.Bytes
	}
	return out, nil
}

func uniqueDownloadName(raw, sha string, used map[string]bool) string {
	name := sanitizeDownloadName(raw)
	key := cases.Fold().String(name)
	if key == "manifest.json" {
		used[key] = true
	}
	if !used[key] {
		used[key] = true
		return name
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	suffix := "__" + sha[:8]
	for ordinal := 0; ; ordinal++ {
		extra := suffix
		if ordinal > 0 {
			extra += fmt.Sprintf("_%d", ordinal+1)
		}
		candidate := fitDownloadName(stem, extra, ext)
		key = cases.Fold().String(candidate)
		if !used[key] {
			used[key] = true
			return candidate
		}
	}
}

func sanitizeDownloadName(raw string) string {
	raw = norm.NFKC.String(filepath.Base(strings.ReplaceAll(raw, "\\", "/")))
	var cleaned strings.Builder
	for _, r := range raw {
		if r == '/' || r == '\\' || unicode.IsControl(r) {
			continue
		}
		cleaned.WriteRune(r)
	}
	name := strings.TrimSpace(cleaned.String())
	if name == "" || name == "." || name == ".." {
		name = "photo"
	}
	ext := filepath.Ext(name)
	return fitDownloadName(strings.TrimSuffix(name, ext), "", ext)
}

func fitDownloadName(stem, suffix, ext string) string {
	available := maxEntryNameBytes - len([]byte(suffix)) - len([]byte(ext))
	if available < 1 {
		ext = ""
		available = maxEntryNameBytes - len([]byte(suffix))
	}
	for len([]byte(stem)) > available {
		_, width := utf8.DecodeLastRuneInString(stem)
		stem = stem[:len(stem)-width]
	}
	if stem == "" {
		stem = "p"
	}
	return stem + suffix + ext
}
