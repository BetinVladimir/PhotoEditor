package interchange

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// QualifyExportRenderFrame rechecks the descriptor's live session analysis and
// the source's accepted snapshot occurrence under the exact export lease.
func (s SQLSchedulerExportInputCatalog) QualifyExportRenderFrame(ctx context.Context,
	node v2draft.LayerRef, raw []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.Claims.DB == nil || s.Claim.validate() != nil || node.RenderFrame == nil ||
		node.ExpectedBinding == nil ||
		(node.Contract != "WorkingImage" && node.Contract != "FaceObservations") ||
		!validImportRef(node.Ref) || !validImportRef(node.ParentRef) ||
		len(raw) < 2 || len(raw) > 8<<20 || rawSHA(raw) != node.Ref.SHA256 {
		return v2input.ErrInvalid
	}
	frame := node.RenderFrame
	if node.ParentRef != frame.Descriptor || !validImportRef(frame.Root) ||
		!validImportRef(frame.Descriptor) || !validImportRef(node.ExpectedBinding.Source) ||
		frame.Learning.Descriptor != frame.Descriptor ||
		frame.SelectedVersion == uuid.Nil || node.ExpectedBinding.AssetVersionID == uuid.Nil ||
		frame.Ordinal < 0 || frame.Ordinal > 4999 ||
		!validImportRefForSHA(frame.ModelLockSHA) ||
		(node.Contract == "WorkingImage" && node.ExpectedBinding.Source != node.Ref) {
		return v2input.ErrInvalid
	}
	if _, err := v2draft.DocumentOccurrenceChildren(raw, node); err != nil {
		return err
	}
	root := v2input.Entry{Ref: frame.Root, Contract: "FrozenRenderRecipe"}
	descriptor := v2input.Entry{Ref: frame.Descriptor, Contract: "DescriptorShard"}
	source := v2input.Entry{Ref: node.ExpectedBinding.Source, Contract: "WorkingImage"}
	child := v2input.Entry{Ref: node.Ref, Contract: node.Contract}
	beforeRoot, err := s.LookupExportDocument(ctx, root)
	if err != nil {
		return err
	}
	beforeDescriptor, err := s.LookupExportDocument(ctx, descriptor)
	if err != nil {
		return err
	}
	beforeSource, err := s.LookupExportDocument(ctx, source)
	if err != nil {
		return err
	}
	beforeChild := beforeSource
	if child != source {
		beforeChild, err = s.LookupExportDocument(ctx, child)
		if err != nil {
			return err
		}
	}
	if beforeChild.ContentType != "application/json" ||
		beforeChild.Bytes != int64(len(raw)) ||
		beforeSource.ContentType != "application/json" ||
		beforeDescriptor.ContentType != "application/json" ||
		beforeRoot.ContentType != "application/json" {
		return v2input.ErrInvalid
	}
	learning, err := json.Marshal(frame.Learning)
	if err != nil {
		return v2input.ErrInvalid
	}
	var allowed bool
	err = s.Claims.DB.QueryRow(ctx, `SELECT photo.v30_export_adoption_render_frame(
 $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15::jsonb,
 $16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27)`,
		s.Claim.Tenant, s.Claim.Workflow, s.Claim.Run, s.Claim.Task,
		s.Claim.Result, s.Claim.Actor, s.Claim.Key, s.Claim.RequestSHA,
		s.Claim.Token, root.Ref.ArtifactID, root.Ref.SHA256,
		descriptor.Ref.ArtifactID, descriptor.Ref.SHA256, frame.SelectedVersion,
		string(learning), frame.ModelLockSHA, frame.Ordinal,
		node.ExpectedBinding.AssetVersionID, source.Ref.ArtifactID, source.Ref.SHA256,
		child.Contract, child.Ref.ArtifactID, child.Ref.SHA256,
		s.Bucket, beforeChild.Key, beforeChild.VersionID, beforeChild.Bytes).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return v2input.ErrInvalid
	}
	for _, item := range []struct {
		entry v2input.Entry
		pin   objectstore.Object
	}{
		{root, beforeRoot}, {descriptor, beforeDescriptor},
		{source, beforeSource}, {child, beforeChild},
	} {
		after, err := s.LookupExportDocument(ctx, item.entry)
		if err != nil || after != item.pin {
			return v2input.ErrInvalid
		}
	}
	return ctx.Err()
}

var _ ExportRenderFrameAuthority = SQLSchedulerExportInputCatalog{}
