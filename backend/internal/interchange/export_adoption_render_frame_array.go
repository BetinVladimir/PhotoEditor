package interchange

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// QualifyExportRenderFrameArray binds a binary child to a qualified descriptor
// frame and the exact current WorkingImage producer.
func (s SQLSchedulerExportInputCatalog) QualifyExportRenderFrameArray(ctx context.Context,
	node v2draft.LayerRef) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.Claims.DB == nil || s.Claim.validate() != nil || node.RenderFrame == nil ||
		node.ExpectedBinding == nil ||
		(node.Contract != "WorkingPixels" && node.Contract != "WorkingAlpha") ||
		!validImportRef(node.Ref) || !validImportRef(node.ParentRef) ||
		node.ParentRef != node.ExpectedBinding.Source {
		return v2input.ErrInvalid
	}
	frame := node.RenderFrame
	if !validImportRef(frame.Root) || !validImportRef(frame.Descriptor) ||
		frame.Learning.Descriptor != frame.Descriptor ||
		frame.SelectedVersion == uuid.Nil || node.ExpectedBinding.AssetVersionID == uuid.Nil ||
		frame.Ordinal < 0 || frame.Ordinal > 4999 ||
		!validImportRefForSHA(frame.ModelLockSHA) {
		return v2input.ErrInvalid
	}
	root := v2input.Entry{Ref: frame.Root, Contract: "FrozenRenderRecipe"}
	descriptor := v2input.Entry{Ref: frame.Descriptor, Contract: "DescriptorShard"}
	source := v2input.Entry{Ref: node.ExpectedBinding.Source, Contract: "WorkingImage"}
	array := v2input.Entry{Ref: node.Ref, Contract: node.Contract}
	pins := make([]objectstore.Object, 4)
	for i, entry := range []v2input.Entry{root, descriptor, source, array} {
		pin, err := s.LookupExportDocument(ctx, entry)
		if err != nil {
			return err
		}
		pins[i] = pin
	}
	if pins[0].ContentType != "application/json" ||
		pins[1].ContentType != "application/json" ||
		pins[2].ContentType != "application/json" ||
		pins[3].ContentType != "application/x-npy" {
		return v2input.ErrInvalid
	}
	learning, err := json.Marshal(frame.Learning)
	if err != nil {
		return v2input.ErrInvalid
	}
	var allowed bool
	err = s.Claims.DB.QueryRow(ctx, `SELECT photo.v30_export_adoption_render_frame_array(
 $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15::jsonb,
 $16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27)`,
		s.Claim.Tenant, s.Claim.Workflow, s.Claim.Run, s.Claim.Task,
		s.Claim.Result, s.Claim.Actor, s.Claim.Key, s.Claim.RequestSHA,
		s.Claim.Token, root.Ref.ArtifactID, root.Ref.SHA256,
		descriptor.Ref.ArtifactID, descriptor.Ref.SHA256, frame.SelectedVersion,
		string(learning), frame.ModelLockSHA, frame.Ordinal,
		node.ExpectedBinding.AssetVersionID, source.Ref.ArtifactID, source.Ref.SHA256,
		array.Contract, array.Ref.ArtifactID, array.Ref.SHA256,
		s.Bucket, pins[3].Key, pins[3].VersionID, pins[3].Bytes).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return v2input.ErrInvalid
	}
	for i, entry := range []v2input.Entry{root, descriptor, source, array} {
		after, err := s.LookupExportDocument(ctx, entry)
		if err != nil || after != pins[i] {
			return v2input.ErrInvalid
		}
	}
	return ctx.Err()
}

var _ ExportRenderFrameArrayAuthority = SQLSchedulerExportInputCatalog{}
