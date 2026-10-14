package provenance

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"slices"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// Parents must come from independently proved adopted parent ImageResults.
// Report status is algorithm evidence, never human review approval. This helper
// does not infer generated=false from absence of an image-level report flag.
func ReadSigningRenderReport(ctx context.Context, documents SigningTaskDocumentReader, who auth.Identity, ready ReadySigningExport, task CanonicalSigningTask, binding v2input.SessionVersionBinding, reference v2input.Ref, parents []v2input.Ref) (v2draft.ImageReportDocument, error) {
	var zero v2draft.ImageReportDocument
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if documents == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || ready.Tenant == uuid.Nil || ready.Project == uuid.Nil || ready.ExportID == uuid.Nil || binding.AssetVersionID == uuid.Nil || !ref(binding.Source) || !digest(binding.CanvasHash) || task.Operation != "render.compose" || task.Version != "1" || normalizeCanonicalSigningTask(&task) != nil || !ref(reference) || parents == nil || len(parents) > 33 {
		return zero, ErrInvalid
	}
	seen := make(map[uuid.UUID]bool, len(parents))
	for _, parent := range parents {
		if !ref(parent) || parent.ArtifactID == reference.ArtifactID || seen[parent.ArtifactID] {
			return zero, ErrInvalid
		}
		seen[parent.ArtifactID] = true
	}
	expected := append([]v2input.Ref{}, parents...)
	raw, err := documents.ReadSigningTaskDocument(ctx, who, ready, task, reference, 1<<20)
	if err != nil {
		return zero, err
	}
	if !pinned(raw, reference, 1<<20) {
		return zero, ErrInvalid
	}
	owned := append([]byte(nil), raw...)
	report, err := v2draft.ParseImageReport(owned, reference)
	if err != nil {
		return zero, ErrInvalid
	}
	if report.Operation != "render.compose" || (report.Status != "pass" && report.Status != "review") || len(report.Sources) != 1 || report.Sources[0] != binding || len(report.Registrations) != 0 || report.ParentReports == nil || !slices.Equal(report.ParentReports, expected) {
		return zero, ErrInvalid
	}
	current, err := documents.ReadSigningTaskDocument(ctx, who, ready, task, reference, 1<<20)
	if err != nil {
		return zero, err
	}
	if !bytes.Equal(current, owned) {
		return zero, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return report, nil
}
