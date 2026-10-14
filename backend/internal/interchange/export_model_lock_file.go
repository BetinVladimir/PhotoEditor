package interchange

import (
	"context"
	"net/url"
	"strings"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type exportModelFile struct {
	Path  string `json:"path"`
	SHA   string `json:"sha256"`
	Bytes int64  `json:"bytes"`
	URL   string `json:"url"`
}
type exportModelBackend struct {
	Status    string  `json:"status"`
	Reason    string  `json:"reason"`
	ReportSHA *string `json:"report_sha256"`
}
type exportModelEntry struct {
	ID                   string                        `json:"id"`
	Architecture         string                        `json:"architecture"`
	WeightID             string                        `json:"weight_id"`
	Repository           string                        `json:"repository"`
	Revision             string                        `json:"revision"`
	Files                []exportModelFile             `json:"files"`
	PreprocessingID      string                        `json:"preprocessing_id"`
	PackagesLockHash     string                        `json:"packages_lock_hash"`
	CodeLicense          string                        `json:"code_license"`
	WeightsLicense       string                        `json:"weights_license"`
	Notices              []string                      `json:"notices"`
	RedistributionPolicy string                        `json:"redistribution_policy"`
	ApprovedForInstall   bool                          `json:"approved_for_install"`
	Backends             map[string]exportModelBackend `json:"backends"`
}
type exportModelLock struct {
	Schema int                `json:"schema_version"`
	LockID string             `json:"lock_id"`
	Models []exportModelEntry `json:"models"`
}

var exportModelIDs = map[string]bool{
	"lraspp_mobilenet_v3_large": true, "sfd": true, "fan2d": true, "identity512": true,
	"eye_open": true, "glare_mask": true, "reflection_rgb": true, "keypointrcnn_resnet50_fpn": true,
	"dinov2_vits14": true, "segformer_b0_ade": true, "bisenet_face19": true, "u2net": true,
	"mobilesam": true, "depth_anything_v2_small": true, "scunet_color_real_psnr": true,
	"realesrgan_x2": true, "realesrgan_x4": true, "big_lama": true, "sd15_inpaint": true,
}
var exportModelHosts = map[string]bool{
	"download.pytorch.org": true, "huggingface.co": true, "cdn-lfs.huggingface.co": true,
	"cdn-lfs-us-1.huggingface.co": true, "cas-bridge.xethub.hf.co": true, "github.com": true,
	"raw.githubusercontent.com": true, "objects.githubusercontent.com": true,
	"release-assets.githubusercontent.com": true,
}

// pin authenticates the complete file; the returned lock_id is the canonical
// payload digest referenced by frozen render recipes and worker ModelLock.
func LoadExportModelLockSHA(ctx context.Context, path, pin string) (string, error) {
	lockID, _, err := LoadExportModelLockWeights(ctx, path, pin)
	return lockID, err
}

// LoadExportModelLockWeights returns an owned model-ID/weight-ID inventory from
// the same authenticated bytes used to establish the canonical lock ID.
func LoadExportModelLockWeights(ctx context.Context, path, pin string) (string, map[string]string, error) {
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	if !validImportRefForSHA(pin) {
		return "", nil, v2input.ErrInvalid
	}
	raw, err := readExportPolicyFile(ctx, path, 1<<20)
	if err != nil {
		return "", nil, err
	}
	if rawSHA(raw) != pin {
		return "", nil, v2input.ErrInvalid
	}
	var lock exportModelLock
	if decodeImportDocument(raw, &lock) != nil || lock.Schema != 1 || !validImportRefForSHA(lock.LockID) || len(lock.Models) > 64 {
		return "", nil, v2input.ErrInvalid
	}
	seen := map[string]bool{}
	weights := map[string]string{}
	for _, model := range lock.Models {
		if !exportModelIDs[model.ID] || seen[model.ID] || model.Architecture == "" || model.WeightID == "" || len(model.WeightID) > 240 || model.Repository == "" || len(model.Revision) < 7 || len(model.Revision) > 128 || model.PreprocessingID == "" || !validImportRefForSHA(model.PackagesLockHash) || model.CodeLicense == "" || model.WeightsLicense == "" || model.Notices == nil || len(model.Files) < 1 || len(model.Files) > 256 || len(model.Backends) != 3 {
			return "", nil, v2input.ErrInvalid
		}
		seen[model.ID] = true
		if model.ApprovedForInstall {
			weights[model.ID] = model.WeightID
		}
		switch model.RedistributionPolicy {
		case "allowed", "restricted", "unresolved":
		default:
			return "", nil, v2input.ErrInvalid
		}
		if model.ApprovedForInstall && (model.RedistributionPolicy == "unresolved" || strings.EqualFold(model.CodeLicense, "unresolved") || strings.EqualFold(model.WeightsLicense, "unresolved")) {
			return "", nil, v2input.ErrInvalid
		}
		paths := map[string]bool{}
		for _, file := range model.Files {
			if len(file.Path) < 1 || len(file.Path) > 240 || strings.Contains(file.Path, "\\") || strings.ContainsRune(file.Path, 0) || strings.HasPrefix(file.Path, "/") || strings.Contains(file.Path, "//") || strings.HasSuffix(file.Path, "/") || paths[file.Path] || !validImportRefForSHA(file.SHA) || file.Bytes < 1 || file.Bytes > 8<<30 {
				return "", nil, v2input.ErrInvalid
			}
			for _, part := range strings.Split(file.Path, "/") {
				if part == "" || part == "." || part == ".." {
					return "", nil, v2input.ErrInvalid
				}
			}
			paths[file.Path] = true
			parsed, e := url.Parse(file.URL)
			if e != nil || parsed.Scheme != "https" || !exportModelHosts[parsed.Hostname()] || (parsed.Port() != "" && parsed.Port() != "443") || parsed.User != nil || parsed.Fragment != "" {
				return "", nil, v2input.ErrInvalid
			}
		}
		for _, backend := range []string{"cpu", "cuda", "mps"} {
			evidence, ok := model.Backends[backend]
			if !ok {
				return "", nil, v2input.ErrInvalid
			}
			switch evidence.Status {
			case "unsupported", "untested", "passed":
			default:
				return "", nil, v2input.ErrInvalid
			}
			if evidence.ReportSHA != nil && !validImportRefForSHA(*evidence.ReportSHA) || evidence.Status == "passed" && evidence.ReportSHA == nil {
				return "", nil, v2input.ErrInvalid
			}
		}
	}
	payload := struct {
		Schema int                `json:"schema_version"`
		Models []exportModelEntry `json:"models"`
	}{lock.Schema, lock.Models}
	actual, err := canonical.Hash(payload)
	if err != nil || actual != lock.LockID {
		return "", nil, v2input.ErrInvalid
	}
	return lock.LockID, weights, ctx.Err()
}
