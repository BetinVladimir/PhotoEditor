package v2learning

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// ReviewedDefectSource is a current SQL-scoped review and canonical producer
// observation. It is not yet a training-example/source-version association.
type ReviewedDefectSource struct {
	Decision       DefectHoldoutDecision
	Image          v2input.Ref
	Proposal       v2input.Ref
	LabelMap       v2input.Ref
	Selection      v2input.Ref
	Resolved       v2input.Ref
	Params         v2input.Ref
	ProposalTaskID uuid.UUID
	RepairTaskID   uuid.UUID
}

// LoadReviewedDefectSource must be called inside a scheduler tenant transaction.
// The SQL function rechecks editor ACL, review revision, canonical producer and
// completed repair. The caller must separately prove event/version lineage and
// physically decode the exact repair Params before publishing evidence.
func LoadReviewedDefectSource(ctx context.Context, tx pgx.Tx,
	tenantID, projectID, actorID, authSessionID, reviewID uuid.UUID) (ReviewedDefectSource, error) {
	if tx == nil || tenantID == uuid.Nil || projectID == uuid.Nil || actorID == uuid.Nil ||
		authSessionID == uuid.Nil || reviewID == uuid.Nil {
		return ReviewedDefectSource{}, ErrInvalidTrainingDraft
	}
	var source ReviewedDefectSource
	source.Decision.TenantID = tenantID
	var selectionRaw []byte
	err := tx.QueryRow(ctx, `SELECT review_id,run_id,actor_id,decision,route,
 decision_hash,revision,selection,image_id,image_sha,proposal_id,proposal_sha,
 labels_id,labels_sha,selection_id,selection_sha,resolved_id,resolved_sha,
 params_id,params_sha,proposal_task_id,repair_task_id
 FROM photo.v23_reviewed_defect_source($1,$2,$3,$4,$5)`,
		tenantID, projectID, actorID, authSessionID, reviewID).Scan(
		&source.Decision.ReviewID, &source.Decision.RunID, &source.Decision.ActorID,
		&source.Decision.Decision, &source.Decision.Route,
		&source.Decision.DecisionHash, &source.Decision.Revision, &selectionRaw,
		&source.Image.ArtifactID, &source.Image.SHA256,
		&source.Proposal.ArtifactID, &source.Proposal.SHA256,
		&source.LabelMap.ArtifactID, &source.LabelMap.SHA256,
		&source.Selection.ArtifactID, &source.Selection.SHA256,
		&source.Resolved.ArtifactID, &source.Resolved.SHA256,
		&source.Params.ArtifactID, &source.Params.SHA256,
		&source.ProposalTaskID, &source.RepairTaskID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReviewedDefectSource{}, ErrInvalidTrainingDraft
	}
	if err != nil {
		return ReviewedDefectSource{}, err
	}
	if err := validateReviewedDefectSource(&source, selectionRaw, reviewID); err != nil {
		return ReviewedDefectSource{}, err
	}
	return source, nil
}

func validateReviewedDefectSource(source *ReviewedDefectSource, raw []byte, expectedReview uuid.UUID) error {
	if source == nil || expectedReview == uuid.Nil || len(raw) < 2 || len(raw) > 1<<20 {
		return ErrInvalidTrainingDraft
	}
	d := &source.Decision
	if d.TenantID == uuid.Nil || d.ReviewID != expectedReview || d.RunID == uuid.Nil ||
		d.ActorID == uuid.Nil || d.Decision != "accept" || d.Route != "repair" ||
		d.Revision < 1 || d.Revision > 9007199254740991 || !validSHA(d.DecisionHash) ||
		!validManifestRef(source.Image) || !validManifestRef(source.Proposal) ||
		!validManifestRef(source.LabelMap) || !validManifestRef(source.Selection) ||
		!validManifestRef(source.Resolved) || !validManifestRef(source.Params) ||
		source.ProposalTaskID == uuid.Nil || source.RepairTaskID == uuid.Nil ||
		source.ProposalTaskID == source.RepairTaskID {
		return ErrInvalidTrainingDraft
	}
	refs := []v2input.Ref{source.Image, source.Proposal, source.LabelMap,
		source.Selection, source.Resolved, source.Params}
	for i := range refs {
		for j := i + 1; j < len(refs); j++ {
			if refs[i].ArtifactID == refs[j].ArtifactID {
				return ErrInvalidTrainingDraft
			}
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&d.Selection) != nil || decoder.Decode(new(any)) != io.EOF ||
		review.ValidateDefectSelectionStructure(d.Selection) != nil ||
		d.Selection.Revision != d.Revision || len(d.Selection.Accepted) == 0 ||
		d.Selection.Image.ID != source.Image.ArtifactID ||
		d.Selection.Image.SHA256 != source.Image.SHA256 ||
		d.Selection.Proposal.ID != source.Proposal.ArtifactID ||
		d.Selection.Proposal.SHA256 != source.Proposal.SHA256 ||
		d.Selection.LabelMap.ID != source.LabelMap.ArtifactID ||
		d.Selection.LabelMap.SHA256 != source.LabelMap.SHA256 {
		return ErrInvalidTrainingDraft
	}
	computed, err := reviewedDefectDecisionHash(*d, d.Selection)
	if err != nil || computed != d.DecisionHash {
		return ErrInvalidTrainingDraft
	}
	canonicalSelection, err := canonical.Marshal(d.Selection)
	if err != nil {
		return ErrInvalidTrainingDraft
	}
	digest := sha256.Sum256(canonicalSelection)
	if hex.EncodeToString(digest[:]) != source.Selection.SHA256 {
		return ErrInvalidTrainingDraft
	}
	return nil
}
