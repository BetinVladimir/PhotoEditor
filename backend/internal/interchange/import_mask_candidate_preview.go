package interchange

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

func selectImportMaskCandidate(candidates []ImportPersonMaskCandidate, overrideID, maskID uuid.UUID) (ImportPersonMaskCandidate, error) {
	var selected ImportPersonMaskCandidate
	found := false
	for _, candidate := range candidates {
		if candidate.Decision.OverrideArtifactID != overrideID || candidate.Decision.MaskArtifactID != maskID {
			continue
		}
		if found {
			return ImportPersonMaskCandidate{}, v2input.ErrInvalid
		}
		selected, found = candidate, true
	}
	if !found {
		return ImportPersonMaskCandidate{}, artifactjson.ErrStale
	}
	return selected, nil
}

func (s ImportPersonMaskCandidateService) RenderPreview(ctx context.Context, who auth.Identity, tenantID, projectID, workflowID, entryID, overrideID, maskID uuid.UUID) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.Objects == nil || overrideID == uuid.Nil || maskID == uuid.Nil {
		return nil, domain.NewError("VALIDATION_FAILED", 422)
	}
	candidates, err := s.ReadEntry(ctx, who, tenantID, projectID, workflowID, entryID)
	if err != nil {
		return nil, err
	}
	selected, err := selectImportMaskCandidate(candidates, overrideID, maskID)
	if err != nil {
		return nil, err
	}
	mask, err := review.ParseMaskRef([]byte(selected.Decision.MaskJCS))
	if err != nil || mask.Selector != "person" || mask.Artifact.ID != maskID {
		return nil, v2input.ErrInvalid
	}
	var parsed importParsedDocument
	if err := decodeImportDocument([]byte(selected.Decision.ParsedJCS), &parsed); err != nil {
		return nil, err
	}
	var path string
	for _, item := range parsed.Manifest.Inventory {
		if item.ID == maskID && item.SHA == mask.Artifact.SHA256 && item.Contract == "MaskPixels" {
			if path != "" {
				return nil, v2input.ErrInvalid
			}
			path = item.Path
		}
	}
	if path == "" {
		return nil, v2input.ErrInvalid
	}
	var staged v2input.Ref
	for _, file := range parsed.Files {
		if file.Path == path && file.Contract == "MaskPixels" && file.Content.SHA256 == mask.Artifact.SHA256 {
			if staged.ArtifactID != uuid.Nil {
				return nil, v2input.ErrInvalid
			}
			staged = file.Content
		}
	}
	if !validImportRef(staged) {
		return nil, v2input.ErrInvalid
	}
	source := SQLImportMaskCandidatePixelSource{DB: s.DB, Tenant: tenantID, Workflow: workflowID, Who: who, Bucket: s.Bucket, Parsed: []byte(selected.Decision.ParsedJCS)}
	object, err := source.Lookup(ctx, maskID, staged)
	if err != nil {
		return nil, err
	}
	reader, err := s.Objects.Open(ctx, object)
	if err != nil {
		if reader != nil {
			_ = reader.Close()
		}
		return nil, err
	}
	if reader == nil {
		return nil, v2input.ErrInvalid
	}
	png, renderErr := masknpy.RenderProbabilityMaskPreview(ctx, reader, object.Bytes, staged.SHA256, mask.Canvas.Width, mask.Canvas.Height, 1024)
	closeErr := reader.Close()
	if renderErr != nil {
		return nil, renderErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	currentObject, err := source.Lookup(ctx, maskID, staged)
	if err != nil {
		return nil, err
	}
	if currentObject != object {
		return nil, artifactjson.ErrStale
	}
	currentCandidates, err := s.ReadEntry(ctx, who, tenantID, projectID, workflowID, entryID)
	if err != nil {
		return nil, err
	}
	current, err := selectImportMaskCandidate(currentCandidates, overrideID, maskID)
	if err != nil {
		return nil, err
	}
	if current.Decision != selected.Decision {
		return nil, artifactjson.ErrStale
	}
	return png, ctx.Err()
}
