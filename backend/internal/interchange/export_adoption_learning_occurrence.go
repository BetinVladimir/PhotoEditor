package interchange

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"

	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// QualifyExportLearningOccurrence repeats the live training-consent or
// descriptor-producer proof for an exact registered frozen recipe. The lease,
// source snapshot and object versions are checked by the private SQL gate.
func (s SQLSchedulerExportInputCatalog) QualifyExportLearningOccurrence(ctx context.Context,
	node v2draft.LayerRef, raw []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.Claims.DB == nil || s.Claim.validate() != nil ||
		(node.Contract != "CandidateProfile" && node.Contract != "DescriptorShard") ||
		!validImportRef(node.Ref) || !validImportRef(node.ParentRef) ||
		node.ExpectedBinding == nil || node.ExpectedLearning == nil ||
		!validImportRef(node.ExpectedBinding.Source) ||
		node.ExpectedBinding.AssetVersionID == uuid.Nil ||
		len(raw) < 2 || len(raw) > 8<<20 || rawSHA(raw) != node.Ref.SHA256 {
		return v2input.ErrInvalid
	}
	if node.Contract == "CandidateProfile" && node.ExpectedLearning.Model != node.Ref {
		return v2input.ErrInvalid
	}
	if node.Contract == "DescriptorShard" &&
		(node.ExpectedLearning.Descriptor != node.Ref ||
			!validImportRefForSHA(node.ExpectedModelLockSHA) ||
			node.ExpectedLearning.Ordinal < 0 || node.ExpectedLearning.Ordinal > 4999) {
		return v2input.ErrInvalid
	}
	root := v2input.Entry{Ref: node.ParentRef, Contract: "FrozenRenderRecipe"}
	child := v2input.Entry{Ref: node.Ref, Contract: node.Contract}
	rootPin, err := s.LookupExportDocument(ctx, root)
	if err != nil {
		return err
	}
	childPin, err := s.LookupExportDocument(ctx, child)
	if err != nil {
		return err
	}
	if childPin.Bytes != int64(len(raw)) || childPin.ContentType != "application/json" ||
		rootPin.ContentType != "application/json" {
		return v2input.ErrInvalid
	}
	learning, err := json.Marshal(node.ExpectedLearning)
	if err != nil {
		return v2input.ErrInvalid
	}
	var qualified bool
	err = s.Claims.DB.QueryRow(ctx, `SELECT photo.v30_export_adoption_learning_occurrence(
 $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15::jsonb,$16,
 $17,$18,$19,$20,$21,$22,$23,$24)`,
		s.Claim.Tenant, s.Claim.Workflow, s.Claim.Run, s.Claim.Task,
		s.Claim.Result, s.Claim.Actor, s.Claim.Key, s.Claim.RequestSHA,
		s.Claim.Token, root.Ref.ArtifactID, root.Ref.SHA256,
		node.ExpectedBinding.AssetVersionID, node.ExpectedBinding.Source.ArtifactID,
		node.ExpectedBinding.Source.SHA256, string(learning), node.ExpectedModelLockSHA,
		child.Contract, child.Ref.ArtifactID, child.Ref.SHA256,
		s.Bucket, childPin.Key, childPin.VersionID, childPin.Bytes,
		rootPin.VersionID).Scan(&qualified)
	if err != nil {
		return err
	}
	if !qualified {
		return v2input.ErrInvalid
	}
	rootAfter, err := s.LookupExportDocument(ctx, root)
	if err != nil {
		return err
	}
	childAfter, err := s.LookupExportDocument(ctx, child)
	if err != nil {
		return err
	}
	if rootAfter != rootPin || childAfter != childPin {
		return v2input.ErrInvalid
	}
	return ctx.Err()
}

var _ ExportLearningOccurrenceAuthority = SQLSchedulerExportInputCatalog{}
