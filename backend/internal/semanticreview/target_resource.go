package semanticreview

import (
	"github.com/google/uuid"
	"time"
)

// TargetRow matches RowSemanticTargets. Revisions stay strings on the wire.
type TargetRow struct {
	ResourceType        string     `json:"resource_type"`
	ID                  uuid.UUID  `json:"id"`
	TenantID            uuid.UUID  `json:"tenant_id"`
	ProjectID           uuid.UUID  `json:"project_id"`
	CreatedAt           time.Time  `json:"created_at"`
	Revision            int64      `json:"revision,string"`
	UpdatedAt           time.Time  `json:"updated_at"`
	IntentID            uuid.UUID  `json:"intent_id"`
	ChildRunID          *uuid.UUID `json:"child_run_id"`
	BatchID             *uuid.UUID `json:"batch_id"`
	AssetVersionID      uuid.UUID  `json:"asset_version_id"`
	CandidateArtifactID *uuid.UUID `json:"candidate_artifact_id"`
	ReviewID            *uuid.UUID `json:"review_id"`
	OverrideID          *uuid.UUID `json:"override_id"`
	State               string     `json:"state"`
}

type TargetDocument struct {
	ArtifactID uuid.UUID `json:"artifact_id"`
	SHA256     string    `json:"sha256"`
}

type TargetResource struct {
	ID       uuid.UUID       `json:"id"`
	Revision int64           `json:"revision,string"`
	Document *TargetDocument `json:"document"`
	Data     TargetRow       `json:"data"`
}

// NewTargetResource owns optional IDs so caller mutations cannot substitute pins.
// Authorization, cursor scope and artifact readiness belong to the read service.
func NewTargetResource(row TargetRow, document *TargetDocument) (TargetResource, error) {
	if row.ID == uuid.Nil || row.TenantID == uuid.Nil || row.ProjectID == uuid.Nil || row.IntentID == uuid.Nil || row.AssetVersionID == uuid.Nil || row.Revision < 1 || row.CreatedAt.IsZero() || row.UpdatedAt.IsZero() || !validTargetState(row.State) {
		return TargetResource{}, ErrInvalid
	}
	if row.ResourceType != "" && row.ResourceType != "semantic_targets" {
		return TargetResource{}, ErrInvalid
	}
	row.ResourceType = "semantic_targets"
	for _, pin := range []**uuid.UUID{&row.ChildRunID, &row.BatchID, &row.CandidateArtifactID, &row.ReviewID, &row.OverrideID} {
		if *pin != nil {
			if **pin == uuid.Nil {
				return TargetResource{}, ErrInvalid
			}
			owned := **pin
			*pin = &owned
		}
	}
	var ownedDocument *TargetDocument
	if document != nil {
		if row.CandidateArtifactID == nil || document.ArtifactID != *row.CandidateArtifactID || !shaPattern.MatchString(document.SHA256) {
			return TargetResource{}, ErrInvalid
		}
		owned := *document
		ownedDocument = &owned
	}
	return TargetResource{row.ID, row.Revision, ownedDocument, row}, nil
}
