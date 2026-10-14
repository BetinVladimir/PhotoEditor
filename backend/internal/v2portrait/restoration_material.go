package v2portrait

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// RestorationDocuments are immutable source bytes resolved by the trusted
// producer. Physical object versions and current producer authority are checked
// separately before these bytes are accepted for publication.
type RestorationDocuments struct {
	Source, Donor, Target, Protected []byte
}

func restorationMask(raw []byte, ref v2input.Ref, image review.WorkingImage,
	protected bool) (review.MaskRef, error) {
	invalid := func() (review.MaskRef, error) { return review.MaskRef{}, v2input.ErrInvalid }
	if len(raw) < 2 || len(raw) > 1048576 {
		return invalid()
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != ref.SHA256 {
		return invalid()
	}
	mask, err := review.ParseMaskRef(raw)
	if err != nil {
		return invalid()
	}
	canonicalMask, err := canonical.Marshal(mask)
	if err != nil || !bytes.Equal(canonicalMask, raw) || mask.Canvas != image.Canvas ||
		!bytes.Equal(mask.TransformChain, image.TransformChain) || mask.Protected != protected {
		return invalid()
	}
	return mask, nil
}

func restorationImageArrays(image review.WorkingImage) ([]v2input.Ref, error) {
	refs := []v2input.Ref{{ArtifactID: image.Pixels.ID, SHA256: image.Pixels.SHA256}}
	if image.Alpha != nil {
		refs = append(refs, v2input.Ref{ArtifactID: image.Alpha.ID, SHA256: image.Alpha.SHA256})
	}
	var chain struct {
		Steps []struct {
			Kind         string              `json:"kind"`
			InverseField *review.ArtifactRef `json:"inverse_field"`
		} `json:"steps"`
	}
	if json.Unmarshal(image.TransformChain, &chain) != nil {
		return nil, v2input.ErrInvalid
	}
	for _, step := range chain.Steps {
		if step.Kind == "dense_warp" {
			if step.InverseField == nil {
				return nil, v2input.ErrInvalid
			}
			refs = append(refs, v2input.Ref{ArtifactID: step.InverseField.ID,
				SHA256: step.InverseField.SHA256})
		}
	}
	return refs, nil
}

// VerifyRestorationMaterialInputs closes the immutable document-to-array edges
// that the draft descriptor alone cannot see. It does no database or object IO.
func VerifyRestorationMaterialInputs(ctx context.Context, draft RestorationDraft,
	paramsRaw []byte, documents RestorationDocuments) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if draft.Task.ResourceClass != "gpu" || draft.Task.OperationVersion != "1" ||
		len(draft.Entries) < 1 {
		return v2input.ErrInvalid
	}
	paramsSum := sha256.Sum256(paramsRaw)
	if hex.EncodeToString(paramsSum[:]) != draft.Task.ParamsSHA {
		return v2input.ErrInvalid
	}
	var p struct {
		Base        v2input.SessionVersionBinding  `json:"base"`
		Donor       *v2input.SessionVersionBinding `json:"donor"`
		Mask        v2input.Ref                    `json:"mask"`
		GlassesMask v2input.Ref                    `json:"glasses_mask"`
		Protected   v2input.Ref                    `json:"protected"`
	}
	if json.Unmarshal(paramsRaw, &p) != nil {
		return v2input.ErrInvalid
	}
	source, err := VerifyDonorWorkingImage(p.Base, documents.Source)
	if err != nil {
		return v2input.ErrInvalid
	}
	if draft.Task.Operation == "image.reflection" &&
		source.Canvas.Width*source.Canvas.Height > 16000000 {
		return v2input.ErrInvalid
	}
	images := []review.WorkingImage{source}
	if draft.Task.Operation == "portrait.glare" && p.Donor != nil {
		donor, e := VerifyDonorWorkingImage(*p.Donor, documents.Donor)
		if e != nil || donor.Color != source.Color {
			return v2input.ErrInvalid
		}
		images = append(images, donor)
	} else if len(documents.Donor) != 0 || p.Donor != nil {
		return v2input.ErrInvalid
	}
	targetRef := p.Mask
	if draft.Task.Operation == "portrait.glare" {
		targetRef = p.GlassesMask
	}
	target, err := restorationMask(documents.Target, targetRef, source, false)
	if err != nil {
		return err
	}
	guard, err := restorationMask(documents.Protected, p.Protected, source, true)
	if err != nil || target.Artifact.ID == guard.Artifact.ID {
		return v2input.ErrInvalid
	}
	required := map[v2input.Ref]bool{
		{ArtifactID: target.Artifact.ID, SHA256: target.Artifact.SHA256}: true,
		{ArtifactID: guard.Artifact.ID, SHA256: guard.Artifact.SHA256}:   true,
	}
	for _, image := range images {
		refs, e := restorationImageArrays(image)
		if e != nil {
			return e
		}
		for _, ref := range refs {
			if ref.ArtifactID == target.Artifact.ID || ref.ArtifactID == guard.Artifact.ID {
				return v2input.ErrInvalid
			}
			required[ref] = true
		}
	}
	declared := map[v2input.Ref]bool{}
	for _, entry := range draft.Entries {
		if entry.Contract == "NpyArray" {
			declared[entry.Ref] = true
		}
	}
	for ref := range required {
		if !declared[ref] {
			return v2input.ErrInvalid
		}
	}
	return ctx.Err()
}

// CompileVerifiedRestorationDraftTask is the publication-facing compiler. It
// never returns a draft unless the supplied immutable documents close every
// source, mask and inverse-warp array reference in the frozen inventory.
func CompileVerifiedRestorationDraftTask(ctx context.Context, workflow uuid.UUID,
	ordinal int, operation string, paramsRaw, baseProfile []byte,
	pin RestorationQualificationPin, paramsRef v2input.Ref,
	inputs []v2input.Entry, documents RestorationDocuments) (RestorationDraft, error) {
	draft, err := CompileRestorationDraftTask(ctx, workflow, ordinal, operation,
		paramsRaw, baseProfile, pin, paramsRef, inputs)
	if err != nil {
		return RestorationDraft{}, err
	}
	if err = VerifyRestorationMaterialInputs(ctx, draft, paramsRaw, documents); err != nil {
		return RestorationDraft{}, err
	}
	return draft, nil
}
