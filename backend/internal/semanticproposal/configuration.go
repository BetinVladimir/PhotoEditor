package semanticproposal

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// validateTransferConfiguration validates deployment inputs before publication.
// It does not replace live SQL, document bytes or protection-policy authority.
func validateTransferConfiguration(c TransferPreparation) error {
	if err := validateTransferPolicyConfiguration(c); err != nil {
		return err
	}
	return validateTransferProtectionRefs(c)
}

// Resource/model policy may be validated before original per-request mask
// locators are loaded. No transfer may compile until complete refs are validated.
func validateTransferPolicyConfiguration(c TransferPreparation) error {
	if !validMetadataBudget(c) || c.Color.sha == "" || c.Expected.Operation != "semantic.transfer" || c.Expected.Version != "1" || (c.Expected.Backend != "cuda" && c.Expected.Backend != "mps") {
		return v2input.ErrInvalid
	}
	if err := verifyQualificationConfiguration(c.Qualification, c.Expected); err != nil {
		return err
	}
	if c.Scene != nil {
		_, sha, err := v2input.DecodeSemanticScenePolicy(c.Scene.Policy)
		if err != nil {
			return err
		}
		if _, ok := c.Expected.EvidenceSHAs[sha]; !ok {
			return v2input.ErrInvalid
		}
		check := false
		for _, name := range c.Expected.RequiredChecks {
			if name == "semantic.scene.farneback_depth_plane_v1" {
				check = true
			}
		}
		if !check {
			return v2input.ErrInvalid
		}
		for i, producer := range []TransferSceneProducer{c.Scene.Scene, c.Scene.Depth} {
			operation := "scene.parse"
			if i == 1 {
				operation = "depth.predict"
			}
			if producer.Expected.Operation != operation || producer.Expected.Version != "1" || producer.Expected.Backend != c.Expected.Backend || producer.Expected.ModelLockSHA != c.Qualification.ModelLockSHA || producer.Report.ArtifactID == uuid.Nil {
				return v2input.ErrInvalid
			}
			for _, digest := range []string{producer.Report.SHA256, producer.Expected.ModelLockSHA, producer.Expected.RuntimeLockSHA, producer.Expected.DeviceProfileSHA, producer.Expected.DatasetSHA} {
				if _, err := domain.ParseDigest(digest); err != nil {
					return v2input.ErrInvalid
				}
			}
			for _, cost := range []int64{producer.Costs.RAMBytes, producer.Costs.VRAMBytes, producer.Costs.ScratchBytes} {
				if cost < 0 || cost > 9007199254740991 {
					return v2input.ErrInvalid
				}
			}
			if producer.Costs.VRAMBytes < 1 || (producer.Expected.Backend == "cuda" && producer.Costs.VRAMBytes > 10000000000) {
				return v2input.ErrInvalid
			}
		}
	}
	if c.PersonProtection != nil {
		policy := c.PersonProtection
		if policy.Expected.Operation != "mask.person" || policy.Expected.Version != "1" || policy.Report.ArtifactID == uuid.Nil {
			return v2input.ErrInvalid
		}
		if _, err := domain.ParseDigest(policy.Report.SHA256); err != nil {
			return v2input.ErrInvalid
		}
		if policy.Expected.Backend != "cpu" && policy.Expected.Backend != "cuda" && policy.Expected.Backend != "mps" {
			return v2input.ErrInvalid
		}
		for _, digest := range []string{policy.Expected.ModelLockSHA, policy.Expected.RuntimeLockSHA, policy.Expected.DeviceProfileSHA, policy.Expected.DatasetSHA} {
			if _, err := domain.ParseDigest(digest); err != nil {
				return v2input.ErrInvalid
			}
		}
	}
	return validateTransferCapacityConfiguration(c)
}

func validateTransferCapacityConfiguration(c TransferPreparation) error {
	if !validMetadataBudget(c) {
		return v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(c.Color.sha); err != nil {
		return v2input.ErrInvalid
	}
	for _, limit := range []int64{c.RAM, c.VRAM, c.Scratch, c.Output} {
		if limit <= 0 || limit > 9007199254740991 {
			return v2input.ErrInvalid
		}
	}
	for _, cost := range []int64{c.Costs.RAMBytes, c.Costs.VRAMBytes, c.Costs.ScratchBytes} {
		if cost < 0 || cost > 9007199254740991 {
			return v2input.ErrInvalid
		}
	}
	return nil
}

func validateTransferProtectionRefs(c TransferPreparation) error {
	roots := append([]v2input.Ref{c.SourceProtection}, c.TargetProtection...)
	if len(c.TargetProtection) < 1 || len(c.TargetProtection) > 5000 {
		return v2input.ErrInvalid
	}
	for _, root := range roots {
		if _, err := domain.ParseDigest(root.SHA256); err != nil || root.ArtifactID == uuid.Nil {
			return v2input.ErrInvalid
		}
	}
	return nil
}
