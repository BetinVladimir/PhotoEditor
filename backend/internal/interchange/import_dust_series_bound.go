package interchange

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// BoundImportDustSeriesAuthority restricts every dust frame to an adopted
// old/new source pair. Semantic remains responsible for camera/sensor identity.
type BoundImportDustSeriesAuthority struct {
	Entry    uuid.UUID
	Old      v2input.SessionVersionBinding
	Target   v2input.SessionVersionBinding
	Bindings []ImportCorrectionBinding
	Parsed   []byte
	Reader   ImportArchiveDocumentReader
	Sources  ImportRecipeSourceQualifier
	Semantic ImportDustSeriesAuthority
}

func (a BoundImportDustSeriesAuthority) pair(ctx context.Context, old review.ArtifactRef) (ImportCorrectionBinding, error) {
	if err := ctx.Err(); err != nil {
		return ImportCorrectionBinding{}, err
	}
	if a.Sources == nil || a.Semantic == nil || a.Entry == uuid.Nil || len(a.Bindings) < 1 {
		return ImportCorrectionBinding{}, v2input.ErrInvalid
	}
	var found *ImportCorrectionBinding
	for i := range a.Bindings {
		item := &a.Bindings[i]
		if item.Old.Source.ArtifactID == old.ID && item.Old.Source.SHA256 == old.SHA256 {
			if found != nil || !validImportRef(item.New.Source) || item.Entry == uuid.Nil {
				return ImportCorrectionBinding{}, v2input.ErrInvalid
			}
			found = item
		}
	}
	if found == nil {
		return ImportCorrectionBinding{}, v2input.ErrInvalid
	}
	if err := a.Sources.QualifyImportRecipeSource(ctx, found.Entry, found.Old, found.New); err != nil {
		return ImportCorrectionBinding{}, err
	}
	return *found, nil
}

func (a BoundImportDustSeriesAuthority) ValidateArchivedDustSeries(ctx context.Context, raw []byte, old v2input.SessionVersionBinding) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if old != a.Old || a.Reader == nil || a.Semantic == nil || len(raw) < 2 || len(raw) > 1<<20 {
		return v2input.ErrInvalid
	}
	if _, err := review.ReadDustProposal(ctx, bytes.NewReader(raw), review.ArtifactRef{ID: uuid.New(), SHA256: rawSHA(raw)}, review.ArtifactRef{ID: old.Source.ArtifactID, SHA256: old.Source.SHA256}); err != nil {
		return err
	}
	var parsed importParsedDocument
	if err := decodeImportDocument(a.Parsed, &parsed); err != nil {
		return err
	}
	files := map[string]v2input.Ref{}
	for _, file := range parsed.Files {
		files[file.Path] = file.Content
	}
	matched := false
	for _, item := range parsed.Manifest.Inventory {
		if item.Contract != "DustCandidates" || item.SHA != rawSHA(raw) {
			continue
		}
		staged := files[item.Path]
		if !validImportRef(staged) || staged.SHA256 != item.SHA {
			return v2input.ErrInvalid
		}
		current, err := a.Reader.ReadImportArchiveDocument(ctx, staged, 1<<20)
		if err != nil {
			return err
		}
		if !bytes.Equal(current, raw) {
			return v2input.ErrInvalid
		}
		matched = true
	}
	if !matched {
		return v2input.ErrInvalid
	}
	if _, err := a.pair(ctx, review.ArtifactRef{ID: old.Source.ArtifactID, SHA256: old.Source.SHA256}); err != nil {
		return err
	}
	return a.Semantic.ValidateArchivedDustSeries(ctx, append([]byte(nil), raw...), old)
}

func (a BoundImportDustSeriesAuthority) MapDustSeriesFrame(ctx context.Context, frame review.ArtifactRef, old, target v2input.SessionVersionBinding) (review.ArtifactRef, error) {
	if old != a.Old || target != a.Target || a.Semantic == nil {
		return review.ArtifactRef{}, v2input.ErrInvalid
	}
	pair, err := a.pair(ctx, frame)
	if err != nil {
		return review.ArtifactRef{}, err
	}
	mapped, err := a.Semantic.MapDustSeriesFrame(ctx, frame, old, target)
	if err != nil {
		return review.ArtifactRef{}, err
	}
	if mapped.ID != pair.New.Source.ArtifactID || mapped.SHA256 != pair.New.Source.SHA256 {
		return review.ArtifactRef{}, v2input.ErrInvalid
	}
	return mapped, ctx.Err()
}

func (a BoundImportDustSeriesAuthority) QualifyPreparedDustSeries(ctx context.Context, raw []byte, target v2input.SessionVersionBinding) error {
	if target != a.Target || a.Semantic == nil || a.Sources == nil || len(raw) < 2 || len(raw) > 1<<20 {
		return v2input.ErrInvalid
	}
	base := review.ArtifactRef{ID: target.Source.ArtifactID, SHA256: target.Source.SHA256}
	if _, err := review.ReadDustProposal(ctx, bytes.NewReader(raw), review.ArtifactRef{ID: uuid.New(), SHA256: rawSHA(raw)}, base); err != nil {
		return err
	}
	// Read only the frame list after the strict parser has checked all fields.
	var header struct {
		Frames []review.ArtifactRef `json:"frames"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return err
	}
	for _, frame := range header.Frames {
		found := false
		for _, item := range a.Bindings {
			if item.New.Source.ArtifactID == frame.ID && item.New.Source.SHA256 == frame.SHA256 {
				if found {
					return v2input.ErrInvalid
				}
				if err := a.Sources.QualifyImportRecipeSource(ctx, item.Entry, item.Old, item.New); err != nil {
					return err
				}
				found = true
			}
		}
		if !found {
			return v2input.ErrInvalid
		}
	}
	return a.Semantic.QualifyPreparedDustSeries(ctx, append([]byte(nil), raw...), target)
}

var _ ImportDustSeriesAuthority = BoundImportDustSeriesAuthority{}
