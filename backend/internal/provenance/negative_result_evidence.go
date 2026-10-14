package provenance

import (
	"bytes"
	"context"
	"github.com/google/uuid"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// ReadSigningNegativeResult ties physical ImageResult bytes to a currently adopted
// canonical root. Child refs are not IO authority or generation/review evidence.
func ReadSigningNegativeResult(ctx context.Context, adoption SigningNegativeAdoptionReader, documents SigningTaskDocumentReader, who auth.Identity, ready ReadySigningExport, task CanonicalSigningTask) (v2input.Ref, v2draft.ImageResultDocument, error) {
	var zero v2draft.ImageResultDocument
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, zero, err
	}
	if adoption == nil || documents == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || ready.Tenant == uuid.Nil || ready.Project == uuid.Nil || ready.ExportID == uuid.Nil || task.Operation != "optical.negative" || task.Version != "1" || normalizeCanonicalSigningTask(&task) != nil {
		return v2input.Ref{}, zero, ErrInvalid
	}
	roots, err := canonicalTaskOutputRefs(task.Outputs)
	if err != nil || len(roots) != 1 {
		return v2input.Ref{}, zero, ErrInvalid
	}
	root, err := adoption.ReadSigningAdoptedNegativeRoot(ctx, who, ready, task)
	if err != nil {
		return v2input.Ref{}, zero, err
	}
	if root != roots[0] {
		return v2input.Ref{}, zero, ErrInvalid
	}
	raw, err := documents.ReadSigningTaskDocument(ctx, who, ready, task, root, 1<<20)
	if err != nil {
		return v2input.Ref{}, zero, err
	}
	if !pinned(raw, root, 1<<20) {
		return v2input.Ref{}, zero, ErrInvalid
	}
	owned := append([]byte(nil), raw...)
	result, err := v2draft.ParseImageResult(owned, root)
	if err != nil || result.GeneratedMask != nil || result.WorkingImage.ArtifactID == root.ArtifactID || result.ChangedMask.ArtifactID == root.ArtifactID || result.Report.ArtifactID == root.ArtifactID || result.WorkingImage.ArtifactID == result.ChangedMask.ArtifactID || result.WorkingImage.ArtifactID == result.Report.ArtifactID || result.ChangedMask.ArtifactID == result.Report.ArtifactID {
		return v2input.Ref{}, zero, ErrInvalid
	}
	current, err := documents.ReadSigningTaskDocument(ctx, who, ready, task, root, 1<<20)
	if err != nil {
		return v2input.Ref{}, zero, err
	}
	if !bytes.Equal(current, owned) {
		return v2input.Ref{}, zero, ErrInvalid
	}
	repeated, err := adoption.ReadSigningAdoptedNegativeRoot(ctx, who, ready, task)
	if err != nil {
		return v2input.Ref{}, zero, err
	}
	if repeated != root {
		return v2input.Ref{}, zero, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, zero, err
	}
	return root, result, nil
}

var _ SigningNegativeAdoptionReader = SQLSigningNegativeAdoptionReader{}
