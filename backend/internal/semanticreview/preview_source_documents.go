package semanticreview

import (
	"crypto/sha256"
	"encoding/hex"
	"photoeditor/backend/internal/v2input"
)

// ResolveExposurePreviewDocuments derives the proposal's exact readable image
// and mask dependencies without constructing ApprovedEdit. Catalog authority,
// transform verification and binary/ICC checks remain independent mandatory gates.
func ResolveExposurePreviewDocuments(p ExposurePreviewParams, candidateBody, intentBody []byte,
	docs ExposureSourceDocuments) (v2input.ExposureDependencies, int64, int64, error) {
	return resolveProposalPreviewDocuments(p, candidateBody, intentBody, docs, "local_exposure")
}

func ResolveRemovalPreviewDocuments(p RemovalPreviewParams, candidateBody, intentBody []byte,
	docs ExposureSourceDocuments) (v2input.ExposureDependencies, int64, int64, error) {
	if _, _, err := FreezeRemovalPreviewParams(p); err != nil {
		return v2input.ExposureDependencies{}, 0, 0, err
	}
	return resolveProposalPreviewDocuments(p.ExposurePreviewParams, candidateBody, intentBody, docs, "remove_object")
}

func ResolveBackgroundCleanupPreviewDocuments(p BackgroundCleanupPreviewParams, candidateBody, intentBody []byte,
	docs ExposureSourceDocuments) (v2input.ExposureDependencies, int64, int64, error) {
	if _, _, err := FreezeBackgroundCleanupPreviewParams(p); err != nil {
		return v2input.ExposureDependencies{}, 0, 0, err
	}
	return resolveProposalPreviewDocuments(p.ExposurePreviewParams, candidateBody, intentBody, docs, "background_cleanup")
}

// ResolveRetouchPreviewDocuments uses proposal authority and never constructs ApprovedEdit.
func ResolveRetouchPreviewDocuments(p RetouchPreviewParams, candidateBody, intentBody []byte,
	docs ExposureSourceDocuments) (v2input.ExposureDependencies, int64, int64, error) {
	if _, _, err := FreezeRetouchPreviewParams(p); err != nil {
		return v2input.ExposureDependencies{}, 0, 0, err
	}
	return resolveProposalPreviewDocuments(p.ExposurePreviewParams, candidateBody, intentBody, docs, "retouch_region")
}

func resolveProposalPreviewDocuments(p ExposurePreviewParams, candidateBody, intentBody []byte,
	docs ExposureSourceDocuments, kind string) (v2input.ExposureDependencies, int64, int64, error) {
	invalid := func() (v2input.ExposureDependencies, int64, int64, error) {
		return v2input.ExposureDependencies{}, 0, 0, ErrInvalid
	}
	if _, _, err := FreezeExposurePreviewParams(p); err != nil {
		return invalid()
	}
	for _, item := range []struct {
		body []byte
		ref  v2input.Ref
	}{{candidateBody, p.Candidate}, {intentBody, p.Intent}} {
		if len(item.body) < 2 || len(item.body) > 1048576 {
			return invalid()
		}
		sum := sha256.Sum256(item.body)
		if hex.EncodeToString(sum[:]) != item.ref.SHA256 {
			return invalid()
		}
	}
	candidate, err := v2input.DecodeSemanticCandidate(candidateBody)
	if err != nil || candidate.State != "proposed" || candidate.Target != p.Source || candidate.Mask == nil || candidate.Transform == nil {
		return invalid()
	}
	intent, err := v2input.DecodeSemanticEditIntent(intentBody)
	if err != nil || intent.Kind != kind || (kind == "local_exposure") != (intent.EV != nil) || !intent.PreservePersons || intent.TargetSnapshotID != p.SnapshotID {
		return invalid()
	}
	source, err := v2input.ResolveSemanticSourceMetadata(p.Source, docs.Source)
	if err != nil {
		return invalid()
	}
	reference, err := v2input.ResolveSemanticSourceMetadata(intent.Reference, docs.Reference)
	if err != nil {
		return invalid()
	}
	mask, err := v2input.ResolveSemanticMaskPixels(*candidate.Mask, docs.Mask, p.Source, docs.Source, false)
	if err != nil {
		return invalid()
	}
	protected, err := v2input.ResolveSemanticMaskPixels(p.Protection, docs.Protection, p.Source, docs.Source, true)
	if err != nil {
		return invalid()
	}
	return v2input.ExposureDependencies{Reference: intent.Reference.Source, Source: source.Inputs, ReferenceImage: reference.Inputs,
		MaskPixels: mask, Protection: p.Protection, ProtectionPixels: protected,
		Geometry: append(append([]v2input.Ref(nil), source.Geometry...), reference.Geometry...)}, source.Width, source.Height, nil
}
