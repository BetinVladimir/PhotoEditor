package interchange

import (
	"context"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Structural installation check only. It performs no IO and does not grant
// runtime/consent admission; actual entry-scoped SQL/native checks remain.
type ImportRecipeInstallationValidator interface{ ValidateImportRecipeInstallation(context.Context) error }

func (f SQLImportSchedulerRecipeFactory) ValidateImportRecipeInstallation(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	validator, ok := f.Authority.(ImportRecipeInstallationValidator)
	if !ok || validator == nil {
		return v2input.ErrInvalid
	}
	return validator.ValidateImportRecipeInstallation(ctx)
}
func (f SQLImportEntryRecipeAuthorityFactory) ValidateImportRecipeInstallation(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if f.Codecs.Params == nil || (f.StyleInstallation == nil && f.Codecs.OverrideContent == nil) {
		return v2input.ErrInvalid
	}
	if f.StyleInstallation == nil {
		if f.Codecs.Style == nil || f.Codecs.Backgrounds == nil {
			return v2input.ErrInvalid
		}
	} else {
		s := f.StyleInstallation
		if !validImportRefForSHA(s.WorkICCSHA) || !validImportRefForSHA(s.StyleRuntimeSHA) || !validImportRefForSHA(s.ImageRuntimeSHA) || s.Models == nil || len(s.Models) > 1024 {
			return v2input.ErrInvalid
		}
		for name, lock := range s.Models {
			if name == "" || len(name) > 240 || !validImportRefForSHA(lock) {
				return v2input.ErrInvalid
			}
		}
	}
	if f.Assets == nil && f.ImageRuntimeSHA == "" && f.StyleInstallation == nil {
		return v2input.ErrInvalid
	}
	if f.ImageRuntimeSHA != "" && !validImportRefForSHA(f.ImageRuntimeSHA) {
		return v2input.ErrInvalid
	}
	if f.MaskSemantics != nil && (f.MaskInstallation != nil || f.PersonMaskQualification != nil) {
		return v2input.ErrInvalid
	}
	if f.MaskSemantics == nil && f.MaskInstallation == nil && f.PersonMaskQualification == nil {
		return v2input.ErrInvalid
	}
	if f.MaskInstallation != nil {
		if _, err := f.MaskInstallation.owned(); err != nil {
			return err
		}
	}
	if f.Codecs.DefectReview != nil && f.Codecs.DefectCandidates == nil {
		return v2input.ErrInvalid
	}
	if f.DustSensors != nil && f.Codecs.DustSeries != nil {
		return v2input.ErrInvalid
	}
	if f.Codecs.DustReview != nil && f.Codecs.DustSeries == nil && f.DustSensors == nil {
		return v2input.ErrInvalid
	}
	check := func(report v2input.Ref, locks v2qualification.DeploymentLocks, operation string) error {
		if !validImportRef(report) || locks.Operation != operation || locks.Version != "1" {
			return v2input.ErrInvalid
		}
		_, err := locks.Expected()
		return err
	}
	if p := f.PersonMaskQualification; p != nil {
		if p.Consent == nil {
			return v2input.ErrInvalid
		}
		if err := check(p.Report, p.Locks, "mask.person"); err != nil {
			return err
		}
		if f.MaskInstallation != nil {
			if _, exists := f.MaskInstallation.Models["person"]; exists {
				return v2input.ErrInvalid
			}
		}
	}
	for _, lock := range f.ModelLocks {
		if !validImportRefForSHA(lock) {
			return v2input.ErrInvalid
		}
	}
	keys := map[ImportOperationVersion]bool{}
	if f.Codecs.Operations != nil {
		if _, err := NewImportOperationRouter(ctx, f.Codecs.Operations.operations); err != nil {
			return err
		}
		for key := range f.Codecs.Operations.operations {
			keys[key] = true
		}
	}
	add := func(operation string) error {
		key := ImportOperationVersion{operation, "1"}
		if keys[key] {
			return v2input.ErrInvalid
		}
		keys[key] = true
		return nil
	}
	if p := f.Codecs.LocalPortrait; p != nil {
		if !validImportRefForSHA(p.RuntimeSHA) || p.Capacity.PixelCount < 1 || p.Capacity.RAMBytes < 1 || p.Capacity.ScratchBytes < 1 || p.Capacity.OutputBytes < 1 || p.Capacity.VRAMBytes < 0 {
			return v2input.ErrInvalid
		}
		if err := add("portrait.hair"); err != nil {
			return err
		}
		if err := add("portrait.clothing"); err != nil {
			return err
		}
	}
	if p := f.Codecs.DenoiseQualification; p != nil {
		if err := check(p.Report, p.Locks, "image.denoise"); err != nil {
			return err
		}
		if err := add("image.denoise"); err != nil {
			return err
		}
		if f.Codecs.DenoiseModelLock != "" && f.Codecs.DenoiseModelLock != p.Locks.ModelLockSHA {
			return v2input.ErrInvalid
		}
	}
	if keys[ImportOperationVersion{"image.denoise", "1"}] && f.Codecs.DenoiseQualification == nil && !validImportRefForSHA(f.Codecs.DenoiseModelLock) {
		return v2input.ErrInvalid
	}
	if f.Codecs.UpscaleQualifications != nil {
		owned, err := f.Codecs.UpscaleQualifications.owned()
		if err != nil {
			return err
		}
		for scale, p := range owned {
			if err := check(p.Report, p.Locks, "image.upscale"); err != nil {
				return err
			}
			if lock, exists := f.Codecs.UpscaleModelLocks[scale]; exists && lock != p.Locks.ModelLockSHA {
				return v2input.ErrInvalid
			}
		}
		for scale := range f.Codecs.UpscaleModelLocks {
			if _, exists := owned[scale]; !exists {
				return v2input.ErrInvalid
			}
		}
		if err := add("image.upscale"); err != nil {
			return err
		}
	} else if keys[ImportOperationVersion{"image.upscale", "1"}] {
		if _, err := ownedImportUpscaleLocks(f.Codecs.UpscaleModelLocks); err != nil {
			return err
		}
	}
	if p := f.Codecs.StatsQualification; p != nil {
		if err := check(p.Report, p.Locks, "color.stats"); err != nil {
			return err
		}
		if err := add("color.stats"); err != nil {
			return err
		}
	}
	if len(keys) == 0 || len(keys) > 1024 {
		return v2input.ErrInvalid
	}
	return ctx.Err()
}
