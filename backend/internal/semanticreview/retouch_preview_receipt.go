package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// RetouchPreviewReceipt is immutable producer linkage; it does not grant Apply.
type RetouchPreviewReceipt struct {
	SchemaVersion     int         `json:"schema_version"`
	ParamsSHA         string      `json:"params_sha"`
	TaskID            uuid.UUID   `json:"task_id"`
	ResultID          uuid.UUID   `json:"result_id"`
	Attempt           int         `json:"attempt"`
	Fence             int64       `json:"fence"`
	Presentation      v2input.Ref `json:"presentation"`
	WorkingImage      v2input.Ref `json:"working_image"`
	Pixels            v2input.Ref `json:"pixels"`
	NumericalEvidence v2input.Ref `json:"numerical_evidence"`
}

func SealRetouchPreviewReceipt(ctx context.Context, db artifactjson.RowQuerier, who auth.Identity,
	run uuid.UUID, physical PhysicalRetouchPreview) (RetouchPreviewReceipt, error) {
	empty := RetouchPreviewReceipt{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if !physical.verified || physical.adapterContext.ArtifactID == uuid.Nil || !shaPattern.MatchString(physical.adapterContext.SHA256) || db == nil || run == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return empty, ErrInvalid
	}
	linkage := physical.linkage
	params := linkage.Presentation.Params
	body, digest, err := FreezeRetouchPreviewParams(params)
	if err != nil {
		return empty, err
	}
	expectedTask := uuid.NewSHA1(run, []byte("semantic.preview_retouch_region:"+params.TargetID.String()))
	if linkage.Document.TaskID != expectedTask {
		return empty, ErrInvalid
	}
	var raw []byte
	err = db.QueryRow(ctx, "SELECT photo.seal_semantic_retouch_preview_receipt($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)",
		params.TenantID, run, who.UserID, who.SessionID, string(body), digest, physical.adapterContext.ArtifactID, physical.adapterContext.SHA256, linkage.Document.TaskID, linkage.Document.ResultID, linkage.Document.Attempt, linkage.Document.Fence, string(linkage.Document.raw),
		string(linkage.Evidence.raw), string(physical.image.ImageBytes)).Scan(&raw)
	if contextErr := ctx.Err(); contextErr != nil {
		return empty, contextErr
	}
	if err != nil {
		return empty, err
	}
	return decodeRetouchPreviewReceipt(raw, linkage, digest)
}

func decodeRetouchPreviewReceipt(raw []byte, linkage RetouchPreviewPresentationLinkage, digest string) (RetouchPreviewReceipt, error) {
	empty := RetouchPreviewReceipt{}
	expected := RetouchPreviewReceipt{SchemaVersion: 1, ParamsSHA: digest, TaskID: linkage.Document.TaskID,
		ResultID: linkage.Document.ResultID, Attempt: linkage.Document.Attempt, Fence: linkage.Document.Fence,
		Presentation: linkage.Document.Source.Ref, WorkingImage: linkage.Presentation.WorkingImage,
		Pixels: linkage.Presentation.Pixels, NumericalEvidence: linkage.Presentation.NumericalEvidence}
	var got RetouchPreviewReceipt
	if len(raw) < 2 || len(raw) > 16384 || decodeSQLReceipt(raw, &got) != nil || got != expected ||
		got.TaskID == uuid.Nil || got.ResultID == uuid.Nil || got.Attempt < 1 || got.Fence < 1 || !shaPattern.MatchString(got.ParamsSHA) {
		return empty, ErrInvalid
	}
	seen := map[uuid.UUID]bool{}
	for _, ref := range []v2input.Ref{got.Presentation, got.WorkingImage, got.Pixels, got.NumericalEvidence} {
		if ref.ArtifactID == uuid.Nil || !shaPattern.MatchString(ref.SHA256) || seen[ref.ArtifactID] {
			return empty, ErrInvalid
		}
		seen[ref.ArtifactID] = true
	}
	return got, nil
}
