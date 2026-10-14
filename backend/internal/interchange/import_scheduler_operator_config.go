package interchange

import (
	"context"
	"io"
	"os"
	"path/filepath"

	"photoeditor/backend/internal/v2input"
)

type importSchedulerOperatorConfig struct {
	Schema string `json:"schema_version"`
	Native struct {
		Path string `json:"path"`
		SHA  string `json:"sha256"`
	} `json:"native"`
	Style struct {
		Path string `json:"path"`
		SHA  string `json:"sha256"`
	} `json:"style"`
	Denoise struct {
		Path string `json:"path"`
		SHA  string `json:"sha256"`
	} `json:"denoise"`
	Upscale struct {
		Path string `json:"path"`
		SHA  string `json:"sha256"`
	} `json:"upscale"`
	Stats struct {
		Path string `json:"path"`
		SHA  string `json:"sha256"`
	} `json:"stats"`
	Person struct {
		Path string `json:"path"`
		SHA  string `json:"sha256"`
	} `json:"person"`
	ArchiveBytes  int64 `json:"max_archive_bytes"`
	ExpandedBytes int64 `json:"max_expanded_bytes"`
	Files         int   `json:"max_files"`
}

func DecodeImportSchedulerOperatorFiles(raw []byte, pin string) (ImportSchedulerOperatorFiles, error) {
	var zero ImportSchedulerOperatorFiles
	if len(raw) < 2 || len(raw) > 64<<10 || !validImportRefForSHA(pin) || rawSHA(raw) != pin {
		return zero, v2input.ErrInvalid
	}
	var wire importSchedulerOperatorConfig
	if err := decodeImportDocument(raw, &wire); err != nil {
		return zero, err
	}
	if wire.Schema != "interchange-import-scheduler-operator/v1" {
		return zero, v2input.ErrInvalid
	}
	files := ImportSchedulerOperatorFiles{
		NativePath: wire.Native.Path, NativeSHA: wire.Native.SHA,
		StylePath: wire.Style.Path, StyleSHA: wire.Style.SHA,
		Recipes: ImportRecipeDeploymentFiles{
			Restoration: ImportRestorationDeploymentFiles{
				DenoiseFile: wire.Denoise.Path, DenoiseSHA: wire.Denoise.SHA,
				UpscaleFile: wire.Upscale.Path, UpscaleSHA: wire.Upscale.SHA,
			},
			Stats:  ImportStatsDeploymentFile{Path: wire.Stats.Path, SHA: wire.Stats.SHA},
			Person: ImportPersonMaskDeploymentFile{Path: wire.Person.Path, SHA: wire.Person.SHA},
		},
		Limits: ImportPlanLimits{ArchiveBytes: wire.ArchiveBytes, ExpandedBytes: wire.ExpandedBytes, Files: wire.Files},
	}
	if err := files.ValidateDeclaration(); err != nil {
		return zero, err
	}
	return files, nil
}

// LoadImportSchedulerOperatorFiles pins the complete declaration before any
// subordinate deployment is opened. A replaced or mutated file is rejected.
func LoadImportSchedulerOperatorFiles(ctx context.Context, path, pin string) (ImportSchedulerOperatorFiles, error) {
	var zero ImportSchedulerOperatorFiles
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || !validImportRefForSHA(pin) {
		return zero, v2input.ErrInvalid
	}
	before, err := os.Lstat(path)
	if err != nil {
		return zero, err
	}
	if !singleLinkImportOperatorFile(before) || !before.Mode().IsRegular() || before.Mode().Perm()&0022 != 0 || before.Size() < 2 || before.Size() > 64<<10 {
		return zero, v2input.ErrInvalid
	}
	file, err := os.Open(path)
	if err != nil {
		return zero, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return zero, err
	}
	if !singleLinkImportOperatorFile(opened) || !os.SameFile(before, opened) || opened.Size() != before.Size() || opened.Mode() != before.Mode() || !opened.ModTime().Equal(before.ModTime()) {
		return zero, v2input.ErrInvalid
	}
	raw, err := io.ReadAll(io.LimitReader(exportContextReader{ctx, file}, (64<<10)+1))
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	after, err := file.Stat()
	if err != nil {
		return zero, err
	}
	current, err := os.Lstat(path)
	if err != nil {
		return zero, err
	}
	if !singleLinkImportOperatorFile(after) || !singleLinkImportOperatorFile(current) || !os.SameFile(opened, after) || !os.SameFile(after, current) || after.Size() != opened.Size() || after.Mode() != opened.Mode() || !after.ModTime().Equal(opened.ModTime()) || current.Size() != opened.Size() || current.Mode() != opened.Mode() || !current.ModTime().Equal(opened.ModTime()) || int64(len(raw)) != opened.Size() {
		return zero, v2input.ErrInvalid
	}
	return DecodeImportSchedulerOperatorFiles(raw, pin)
}
