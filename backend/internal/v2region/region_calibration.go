package v2region

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// RegionQualification identifies the live operator-selected Region policy.
// Resolver must verify tenant/project authority, trusted passed report state,
// license clearance, operation availability and installed calibration allowlist.
type RegionQualification struct {
	RuntimeSHA string `json:"runtime_lock_sha"`
	ModelSHA   string `json:"model_lock_sha"`
	ICCSHA     string `json:"icc_lock_sha"`
	DeviceSHA  string `json:"device_profile_sha"`
	DatasetSHA string `json:"dataset_sha"`
	PolicySHA  string `json:"policy_sha"`
	Backend    string `json:"backend"`
}
type RegionQualificationResolver interface {
	ResolveRegionQualification(context.Context, FrozenRegionPlan) (RegionQualification, error)
}
type RegionOperatorPeak struct {
	RAMBytes     int64 `json:"ram_bytes"`
	ScratchBytes int64 `json:"scratch_bytes"`
	VRAMBytes    int64 `json:"vram_bytes"`
	OutputBytes  int64 `json:"output_bytes"`
}
type RegionCalibrationSample struct {
	Width  int64              `json:"width"`
	Height int64              `json:"height"`
	Peak   RegionOperatorPeak `json:"operator_peak"`
}
type RegionCalibration struct {
	Qualification RegionQualification       `json:"qualification"`
	WorkloadSHA   string                    `json:"workload_sha"`
	EvidenceSHA   string                    `json:"evidence_sha"`
	Samples       []RegionCalibrationSample `json:"samples"`
}
type regionCalibrationKey struct {
	Qualification RegionQualification
	WorkloadSHA   string
}

// PinnedRegionCalibrationCatalog owns independently pinned deployment bytes.
// Measurements must bound the complete effective style across qualified input
// workloads, not just one favorable image. Digest verification does not measure
// GPU costs or independently establish the qualification resolver's authority.
type PinnedRegionCalibrationCatalog struct {
	profiles    map[regionCalibrationKey]RegionCalibration
	manifestSHA string
}

// RegionWorkloadSHA excludes source identity/ROI to reuse measured full-pipeline
// bounds for the same immutable style/correction/runtime model and ICC contract.
// All nested style inputs are covered by the immutable base-style digest.
func RegionWorkloadSHA(plan FrozenRegionPlan) (string, error) {
	if _, err := plan.SourceShape(); err != nil {
		return "", err
	}
	doc, err := plan.decode()
	if err != nil {
		return "", err
	}
	return canonical.Hash(struct {
		BaseStyle v2input.Ref              `json:"base_style"`
		Delta     v2draft.CorrectionVector `json:"effective_delta"`
		Renderer  string                   `json:"renderer_version"`
		Model     string                   `json:"model_lock_sha"`
		ICC       string                   `json:"icc_lock_sha"`
	}{doc.FrozenRecipe.BaseStyle, doc.FrozenRecipe.EffectiveDelta, doc.FrozenRecipe.Renderer, doc.FrozenRecipe.ModelLockSHA, doc.FrozenRecipe.ICCLockSHA})
}
func (q RegionQualification) valid() bool {
	if q.Backend != "cuda" && q.Backend != "mps" {
		return false
	}
	for _, s := range []string{q.RuntimeSHA, q.ModelSHA, q.ICCSHA, q.DeviceSHA, q.DatasetSHA, q.PolicySHA} {
		if !validRegionSHA(s) {
			return false
		}
	}
	return true
}
func (p RegionCalibration) valid() bool {
	if !p.Qualification.valid() || !validRegionSHA(p.WorkloadSHA) || !validRegionSHA(p.EvidenceSHA) || len(p.Samples) < 1 || len(p.Samples) > 256 {
		return false
	}
	seen := map[[2]int64]bool{}
	for _, s := range p.Samples {
		key := [2]int64{s.Width, s.Height}
		if seen[key] || s.Width < 1 || s.Height < 1 || s.Width > 200000000 || s.Height > 200000000/s.Width || s.Peak.VRAMBytes < 1 {
			return false
		}
		seen[key] = true
		for _, n := range []int64{s.Peak.RAMBytes, s.Peak.ScratchBytes, s.Peak.VRAMBytes, s.Peak.OutputBytes} {
			if n < 0 || n > 9007199254740991 {
				return false
			}
		}
	}
	return true
}
func regionExactObject(raw json.RawMessage, keys ...string) (map[string]json.RawMessage, error) {
	var out map[string]json.RawMessage
	if json.Unmarshal(raw, &out) != nil || len(out) != len(keys) {
		return nil, ErrRegionInvalid
	}
	for _, key := range keys {
		value, ok := out[key]
		if !ok || bytes.Equal(value, []byte("null")) {
			return nil, ErrRegionInvalid
		}
	}
	return out, nil
}
func validateRegionCalibrationShape(raw []byte) error {
	root, err := regionExactObject(raw, "schema_version", "profiles")
	if err != nil {
		return err
	}
	var profiles []json.RawMessage
	if json.Unmarshal(root["profiles"], &profiles) != nil {
		return ErrRegionInvalid
	}
	for _, r := range profiles {
		p, err := regionExactObject(r, "qualification", "workload_sha", "evidence_sha", "samples")
		if err != nil {
			return err
		}
		if _, err = regionExactObject(p["qualification"], "runtime_lock_sha", "model_lock_sha", "icc_lock_sha", "device_profile_sha", "dataset_sha", "policy_sha", "backend"); err != nil {
			return err
		}
		var samples []json.RawMessage
		if json.Unmarshal(p["samples"], &samples) != nil {
			return ErrRegionInvalid
		}
		for _, r := range samples {
			s, err := regionExactObject(r, "width", "height", "operator_peak")
			if err != nil {
				return err
			}
			if _, err = regionExactObject(s["operator_peak"], "ram_bytes", "scratch_bytes", "vram_bytes", "output_bytes"); err != nil {
				return err
			}
		}
	}
	return nil
}
func NewPinnedRegionCalibrationCatalog(raw []byte, expectedSHA string) (*PinnedRegionCalibrationCatalog, error) {
	if len(raw) < 2 || len(raw) > 1048576 || !validRegionSHA(expectedSHA) {
		return nil, ErrRegionInvalid
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != expectedSHA {
		return nil, ErrRegionInvalid
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normalized, raw) {
		return nil, ErrRegionInvalid
	}
	if err = validateRegionCalibrationShape(raw); err != nil {
		return nil, err
	}
	var manifest struct {
		SchemaVersion int                 `json:"schema_version"`
		Profiles      []RegionCalibration `json:"profiles"`
	}
	if json.Unmarshal(raw, &manifest) != nil || manifest.SchemaVersion != 1 || len(manifest.Profiles) < 1 || len(manifest.Profiles) > 128 {
		return nil, ErrRegionInvalid
	}
	result := &PinnedRegionCalibrationCatalog{profiles: map[regionCalibrationKey]RegionCalibration{}, manifestSHA: expectedSHA}
	for _, p := range manifest.Profiles {
		key := regionCalibrationKey{p.Qualification, p.WorkloadSHA}
		if !p.valid() {
			return nil, ErrRegionInvalid
		}
		if _, ok := result.profiles[key]; ok {
			return nil, ErrRegionInvalid
		}
		p.Samples = append([]RegionCalibrationSample(nil), p.Samples...)
		result.profiles[key] = p
	}
	return result, nil
}
func LoadPinnedRegionCalibrationCatalog(path, expectedSHA string) (*PinnedRegionCalibrationCatalog, error) {
	if !filepath.IsAbs(path) {
		return nil, ErrRegionInvalid
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, ErrRegionInvalid
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 2 || info.Size() > 1048576 {
		return nil, ErrRegionInvalid
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 {
		return nil, ErrRegionInvalid
	}
	raw, err := io.ReadAll(io.LimitReader(file, 1048577))
	if err != nil || int64(len(raw)) != info.Size() {
		return nil, ErrRegionInvalid
	}
	after, err := file.Stat()
	if err != nil || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
		return nil, ErrRegionInvalid
	}
	return NewPinnedRegionCalibrationCatalog(raw, expectedSHA)
}

// Select preserves every measured peak, without pixel-ratio downscaling or
// orientation rotation. Qualified input complexity is bounded by the evidence.
func (c *PinnedRegionCalibrationCatalog) Select(ctx context.Context, q RegionQualification, plan FrozenRegionPlan) (RegionQualifiedCost, error) {
	if err := ctx.Err(); err != nil {
		return RegionQualifiedCost{}, err
	}
	if c == nil || !q.valid() {
		return RegionQualifiedCost{}, ErrRegionUnavailable
	}
	shape, err := plan.SourceShape()
	if err != nil {
		return RegionQualifiedCost{}, err
	}
	workload, err := RegionWorkloadSHA(plan)
	if err != nil {
		return RegionQualifiedCost{}, err
	}
	doc, err := plan.decode()
	if err != nil {
		return RegionQualifiedCost{}, err
	}
	if q.ModelSHA != doc.FrozenRecipe.ModelLockSHA || q.ICCSHA != doc.FrozenRecipe.ICCLockSHA {
		return RegionQualifiedCost{}, ErrRegionUnavailable
	}
	p, ok := c.profiles[regionCalibrationKey{q, workload}]
	if !ok {
		return RegionQualifiedCost{}, ErrRegionUnavailable
	}
	var selected *RegionCalibrationSample
	for i := range p.Samples {
		s := &p.Samples[i]
		if s.Width < shape.Width || s.Height < shape.Height {
			continue
		}
		if selected == nil || s.Width*s.Height < selected.Width*selected.Height || s.Width*s.Height == selected.Width*selected.Height && s.Width < selected.Width {
			selected = s
		}
	}
	if selected == nil {
		return RegionQualifiedCost{}, ErrRegionUnavailable
	}
	peak := selected.Peak
	calibrationSHA, err := canonical.Hash(p)
	if err != nil {
		return RegionQualifiedCost{}, ErrRegionInvalid
	}
	pin := RegionCalibrationPin{Qualification: q, CatalogSHA: c.manifestSHA, CalibrationSHA: calibrationSHA,
		WorkloadSHA: workload, EvidenceSHA: p.EvidenceSHA, Sample: *selected}
	return RegionQualifiedCost{RuntimeSHA: q.RuntimeSHA, ModelLockSHA: q.ModelSHA, ICCLockSHA: q.ICCSHA, PolicySHA: q.PolicySHA,
		RAMBytes: peak.RAMBytes, ScratchBytes: peak.ScratchBytes, VRAMBytes: peak.VRAMBytes, OutputBytes: peak.OutputBytes, Pin: pin}, nil
}

// CalibratedRegionCosts requires both live policy authority and an independently
// pinned Region-specific catalog; Render measurements cannot qualify Region.
type CalibratedRegionCosts struct {
	Resolver RegionQualificationResolver
	Catalog  *PinnedRegionCalibrationCatalog
}

func (p CalibratedRegionCosts) QualifyRegionCost(ctx context.Context, plan FrozenRegionPlan) (RegionQualifiedCost, error) {
	if err := ctx.Err(); err != nil {
		return RegionQualifiedCost{}, err
	}
	if p.Resolver == nil || p.Catalog == nil {
		return RegionQualifiedCost{}, ErrRegionUnavailable
	}
	if _, err := plan.SourceShape(); err != nil {
		return RegionQualifiedCost{}, err
	}
	owned := plan
	owned.body = append([]byte(nil), plan.body...)
	owned.inputs = append(owned.inputs[:0:0], plan.inputs...)
	q, err := p.Resolver.ResolveRegionQualification(ctx, owned)
	if err != nil {
		return RegionQualifiedCost{}, err
	}
	return p.Catalog.Select(ctx, q, plan)
}
