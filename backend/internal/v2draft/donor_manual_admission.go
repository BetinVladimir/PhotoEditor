package v2draft

import (
	"context"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// DonorManualAdmissionBinding owns exact original producer identities derived
// from validated documents. It is not relational authority or pixel verification.
type DonorManualAdmissionBinding struct {
	root, original, targetPixel, guardPixel v2input.Ref
	vectors                                 *v2input.Ref
}

func CompileDonorManualAdmissionBinding(ctx context.Context, loaded LoadedDonorTaskPlan, prepared PreparedDonorInputEvidence) (DonorManualAdmissionBinding, error) {
	var zero DonorManualAdmissionBinding
	params, err := validateLoadedDonorPlan(ctx, loaded)
	if err != nil {
		return zero, err
	}
	var rootDoc, originalDoc DocumentWitness
	roots, originals := 0, 0
	documents := make([]DocumentWitness, 0, len(prepared.Masks.Documents))
	seen := make(map[v2input.Ref]DocumentWitness)
	for _, doc := range prepared.Masks.Documents {
		if prior, exists := seen[doc.Ref]; exists {
			if prior.Contract != doc.Contract || prior.JCS != doc.JCS {
				return zero, v2input.ErrInvalid
			}
			continue
		}
		seen[doc.Ref] = doc
		documents = append(documents, doc)
	}
	for _, doc := range documents {
		if doc.Contract == "RasterizedMask" {
			rootDoc = doc
			roots++
		}
		if doc.Contract == "Params" {
			originalDoc = doc
			originals++
		}
	}
	if roots != 1 || originals != 1 {
		return zero, v2input.ErrInvalid
	}
	root, err := review.ParseManualRasterDocument([]byte(rootDoc.JCS), review.ArtifactRef{ID: rootDoc.Ref.ArtifactID, SHA256: rootDoc.Ref.SHA256})
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	if err = review.VerifyManualMaskDocumentRefs(root, review.ArtifactRef{ID: rootDoc.Ref.ArtifactID, SHA256: rootDoc.Ref.SHA256}, review.ArtifactRef{ID: params.EyesMask.ArtifactID, SHA256: params.EyesMask.SHA256}, review.ArtifactRef{ID: params.Protected.ArtifactID, SHA256: params.Protected.SHA256}); err != nil {
		return zero, v2input.ErrInvalid
	}
	vectorRef := v2input.Ref{ArtifactID: root.Vectors.ID, SHA256: root.Vectors.SHA256}
	var vectorRaw []byte
	if vectorRef != originalDoc.Ref {
		count := 0
		for _, doc := range documents {
			if doc.Contract == "ManualMask" && doc.Ref == vectorRef {
				vectorRaw = []byte(doc.JCS)
				count++
			}
		}
		if count != 1 {
			return zero, v2input.ErrInvalid
		}
	}
	origin, err := CompileManualRasterVectors(params.Base.Source, loaded.Images.SourceRaw, rootDoc.Ref, []byte(rootDoc.JCS), originalDoc.Ref, []byte(originalDoc.JCS), vectorRaw)
	if err != nil {
		return zero, err
	}
	value := DonorManualAdmissionBinding{root: rootDoc.Ref, original: originalDoc.Ref, targetPixel: v2input.Ref{ArtifactID: root.Target.Artifact.ID, SHA256: root.Target.Artifact.SHA256}, guardPixel: v2input.Ref{ArtifactID: root.Protected.Artifact.ID, SHA256: root.Protected.Artifact.SHA256}}
	if origin.Contract == "ManualMask" {
		ref := origin.Ref
		value.vectors = &ref
	}
	required := map[v2input.Ref]string{value.root: "RasterizedMask", value.original: "Params", value.targetPixel: "NpyArray", value.guardPixel: "NpyArray", params.EyesMask: "MaskRef", params.Protected: "MaskRef"}
	if len(required) != 6 {
		return zero, v2input.ErrInvalid
	}
	if value.vectors != nil {
		if _, exists := required[*value.vectors]; exists {
			return zero, v2input.ErrInvalid
		}
		required[*value.vectors] = "ManualMask"
	}
	for _, entry := range prepared.Entries {
		if contract, exists := required[entry.Ref]; exists {
			if entry.Contract != contract {
				return zero, v2input.ErrInvalid
			}
			delete(required, entry.Ref)
		}
	}
	if len(required) != 0 {
		return zero, v2input.ErrInvalid
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	return value, nil
}
