package v2region

import (
	"bytes"
	"encoding/json"
)

// RegionCalibrationPin freezes the precise live policy, installed catalog,
// complete calibration profile and covering measured sample selected by the
// server. Canonical profile hashing and catalog membership are established by
// the trusted qualifier; syntax checks alone cannot establish qualification.
type RegionCalibrationPin struct {
	Qualification  RegionQualification     `json:"qualification"`
	CatalogSHA     string                  `json:"catalog_sha"`
	CalibrationSHA string                  `json:"calibration_sha"`
	WorkloadSHA    string                  `json:"workload_sha"`
	EvidenceSHA    string                  `json:"evidence_sha"`
	Sample         RegionCalibrationSample `json:"sample"`
}

func (p RegionCalibrationPin) validate(plan FrozenRegionPlan, cost RegionQualifiedCost) error {
	q := p.Qualification
	if !q.valid() || q.RuntimeSHA != cost.RuntimeSHA || q.ModelSHA != cost.ModelLockSHA || q.ICCSHA != cost.ICCLockSHA || q.PolicySHA != cost.PolicySHA {
		return ErrRegionInvalid
	}
	for _, sha := range []string{p.CatalogSHA, p.CalibrationSHA, p.WorkloadSHA, p.EvidenceSHA} {
		if !validRegionSHA(sha) {
			return ErrRegionInvalid
		}
	}
	workload, err := RegionWorkloadSHA(plan)
	if err != nil || p.WorkloadSHA != workload {
		return ErrRegionInvalid
	}
	shape, err := plan.SourceShape()
	if err != nil {
		return ErrRegionInvalid
	}
	sample := p.Sample
	if sample.Width < shape.Width || sample.Height < shape.Height || sample.Width > 200000000 || sample.Height > 200000000/sample.Width {
		return ErrRegionInvalid
	}
	if sample.Peak != (RegionOperatorPeak{RAMBytes: cost.RAMBytes, ScratchBytes: cost.ScratchBytes, VRAMBytes: cost.VRAMBytes, OutputBytes: cost.OutputBytes}) {
		return ErrRegionInvalid
	}
	return nil
}

// Validate the frozen proof and its complete compiler-owned projection before
// any input resolution/publication. Other installer-owned profile keys remain
// preserved; they cannot replace the compiler's budget or runtime fields.
func (p FrozenRegionPlan) validatePinnedProfile(fields map[string]json.RawMessage) error {
	raw, present := fields["region_qualification"]
	if !present {
		return ErrRegionUnavailable
	}
	proof, err := regionExactObject(raw, "qualification", "catalog_sha", "calibration_sha", "workload_sha", "evidence_sha", "sample")
	if err != nil {
		return err
	}
	if _, err = regionExactObject(proof["qualification"], "runtime_lock_sha", "model_lock_sha", "icc_lock_sha", "device_profile_sha", "dataset_sha", "policy_sha", "backend"); err != nil {
		return err
	}
	sample, err := regionExactObject(proof["sample"], "width", "height", "operator_peak")
	if err != nil {
		return err
	}
	if _, err = regionExactObject(sample["operator_peak"], "ram_bytes", "scratch_bytes", "vram_bytes", "output_bytes"); err != nil {
		return err
	}
	var pin RegionCalibrationPin
	if json.Unmarshal(raw, &pin) != nil {
		return ErrRegionInvalid
	}
	q := pin.Qualification
	peak := pin.Sample.Peak
	cost := RegionQualifiedCost{RuntimeSHA: q.RuntimeSHA, ModelLockSHA: q.ModelSHA, ICCLockSHA: q.ICCSHA, PolicySHA: q.PolicySHA,
		RAMBytes: peak.RAMBytes, ScratchBytes: peak.ScratchBytes, VRAMBytes: peak.VRAMBytes, OutputBytes: peak.OutputBytes, Pin: pin}
	expected, err := CompileRegionExecutionPolicy(p, cost)
	if err != nil {
		return err
	}
	var projection map[string]json.RawMessage
	if json.Unmarshal(expected.Profile, &projection) != nil {
		return ErrRegionInvalid
	}
	for key, value := range projection {
		actual, ok := fields[key]
		if !ok {
			return ErrRegionInvalid
		}
		// Both maps come from canonical whole-object profiles. The canonical
		// library only accepts object/array roots, not standalone strings.
		if !bytes.Equal(actual, value) {
			return ErrRegionInvalid
		}
	}
	return nil
}
