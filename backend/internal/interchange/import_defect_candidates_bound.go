package interchange

import (
	"bytes"
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// Binds a semantic defect-proposal authority to one adopted entry. The
// archive bytes and old/new source pair are independently rechecked on every
// qualification, including prepared closure reads.
type BoundImportDefectCandidatesQualifier struct {
	Entry    uuid.UUID
	Old      v2input.SessionVersionBinding
	Target   v2input.SessionVersionBinding
	Parsed   []byte
	Reader   ImportArchiveDocumentReader
	Sources  ImportRecipeSourceQualifier
	Semantic ImportDefectCandidatesQualifier
}

func (q BoundImportDefectCandidatesQualifier) validate(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if q.Entry == uuid.Nil || q.Reader == nil || q.Sources == nil || q.Semantic == nil || !validImportRef(q.Old.Source) || !validImportRef(q.Target.Source) || q.Old.AssetVersionID == uuid.Nil || q.Target.AssetVersionID == uuid.Nil || q.Old.AssetVersionID == q.Target.AssetVersionID || q.Old.CanvasHash != q.Target.CanvasHash {
		return v2input.ErrInvalid
	}
	_, err := validateParsedImportDocument(ctx, q.Parsed)
	return err
}

func (q BoundImportDefectCandidatesQualifier) ValidateArchivedImportDefectCandidates(ctx context.Context, raw []byte, old v2input.SessionVersionBinding) error {
	if err := q.validate(ctx); err != nil {
		return err
	}
	if old != q.Old || len(raw) < 2 || len(raw) > 1<<20 {
		return v2input.ErrInvalid
	}
	var parsed importParsedDocument
	if err := decodeImportDocument(q.Parsed, &parsed); err != nil {
		return err
	}
	files := map[string]v2input.Ref{}
	for _, file := range parsed.Files {
		files[file.Path] = file.Content
	}
	matched := false
	for _, item := range parsed.Manifest.Inventory {
		if item.Contract != "DefectCandidates" || item.SHA != rawSHA(raw) {
			continue
		}
		staged := files[item.Path]
		if !validImportRef(staged) || staged.SHA256 != item.SHA {
			return v2input.ErrInvalid
		}
		current, err := q.Reader.ReadImportArchiveDocument(ctx, staged, 1<<20)
		if err != nil {
			return err
		}
		if !bytes.Equal(current, raw) {
			return v2input.ErrInvalid
		}
		if _, err := review.ParseDefectProposalStructure(ctx, raw, review.ArtifactRef{ID: item.ID, SHA256: item.SHA}); err != nil {
			return err
		}
		matched = true
	}
	if !matched {
		return v2input.ErrInvalid
	}
	if err := q.Sources.QualifyImportRecipeSource(ctx, q.Entry, q.Old, q.Target); err != nil {
		return err
	}
	return q.Semantic.ValidateArchivedImportDefectCandidates(ctx, append([]byte(nil), raw...), old)
}

func (q BoundImportDefectCandidatesQualifier) QualifyImportedDefectCandidates(ctx context.Context, raw []byte, target v2input.SessionVersionBinding) error {
	if err := q.validate(ctx); err != nil {
		return err
	}
	if target != q.Target || len(raw) < 2 || len(raw) > 1<<20 {
		return v2input.ErrInvalid
	}
	if err := q.Sources.QualifyImportRecipeSource(ctx, q.Entry, q.Old, q.Target); err != nil {
		return err
	}
	if _, err := review.ParseDefectProposalStructure(ctx, raw, review.ArtifactRef{ID: uuid.New(), SHA256: rawSHA(raw)}); err != nil {
		return err
	}
	return q.Semantic.QualifyImportedDefectCandidates(ctx, append([]byte(nil), raw...), target)
}

var _ ImportDefectCandidatesQualifier = BoundImportDefectCandidatesQualifier{}
