package v2learning

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/domain"
)

type TrainingCandidate struct {
	EventID            uuid.UUID
	SessionID          uuid.UUID
	AssetVersionID     uuid.UUID
	SourceContentSHA   string
	SourceLineageSHA   string
	BaseDocumentID     uuid.UUID
	BaseArtifactID     uuid.UUID
	BaseSHA            string
	ApprovedDocumentID uuid.UUID
	ApprovedArtifactID uuid.UUID
	ApprovedSHA        string
	ApprovalID         *uuid.UUID
	ApprovedAt         time.Time
	ConsentID          uuid.UUID
	ConsentEpoch       int64
	ChangedPaths       []string
	ConsentLive        bool
	SourceReady        bool
	BaseReady          bool
	ApprovedReady      bool
}

// LoadTrainingCandidates must run inside the same transaction that freezes
// the snapshot. The SQL entrypoint locks the tenant and profile, so grant
// revocation cannot pass between this read and the eventual freeze commit.
func LoadTrainingCandidates(ctx context.Context, tx pgx.Tx,
	tenantID, profileID uuid.UUID, revision int64, sessionIDs []uuid.UUID,
	actorID, authSessionID uuid.UUID) ([]TrainingCandidate, error) {
	if tx == nil || tenantID == uuid.Nil || profileID == uuid.Nil ||
		actorID == uuid.Nil || authSessionID == uuid.Nil || revision < 1 ||
		len(sessionIDs) < 6 || len(sessionIDs) > 5000 {
		return nil, domain.NewError("VALIDATION_FAILED", 422)
	}
	rows, err := tx.Query(ctx, `SELECT event_id,session_id,asset_version_id,
	 source_content_sha,source_lineage_sha,base_document_id,base_artifact_id,
	 base_sha,approved_document_id,approved_artifact_id,approved_sha,
	 approval_id,approved_at,consent_id,consent_epoch,changed_paths,consent_live,
	 source_ready,base_ready,approved_ready
	 FROM photo.v23_training_candidate_rows($1,$2,$3,$4,$5,$6)`,
		tenantID, profileID, revision, sessionIDs, actorID, authSessionID)
	if err != nil {
		return nil, correctionError(err)
	}
	defer rows.Close()
	out := make([]TrainingCandidate, 0)
	seen := make(map[uuid.UUID]struct{})
	for rows.Next() {
		var row TrainingCandidate
		var rawPaths []byte
		if err = rows.Scan(&row.EventID, &row.SessionID,
			&row.AssetVersionID, &row.SourceContentSHA,
			&row.SourceLineageSHA, &row.BaseDocumentID,
			&row.BaseArtifactID, &row.BaseSHA,
			&row.ApprovedDocumentID, &row.ApprovedArtifactID,
			&row.ApprovedSHA, &row.ApprovalID, &row.ApprovedAt,
			&row.ConsentID, &row.ConsentEpoch, &rawPaths, &row.ConsentLive,
			&row.SourceReady, &row.BaseReady,
			&row.ApprovedReady); err != nil {
			return nil, err
		}
		if row.EventID == uuid.Nil || row.SessionID == uuid.Nil ||
			row.AssetVersionID == uuid.Nil || row.BaseDocumentID == uuid.Nil ||
			row.ApprovedDocumentID == uuid.Nil || row.BaseArtifactID == uuid.Nil ||
			row.ApprovedArtifactID == uuid.Nil || row.ConsentID == uuid.Nil ||
			row.ConsentEpoch < 1 || !validSHA(row.SourceContentSHA) ||
			!validSHA(row.SourceLineageSHA) || !validSHA(row.BaseSHA) ||
			!validSHA(row.ApprovedSHA) || len(out) >= 5000 {
			return nil, domain.NewError("DOCUMENT_INVALID", 422)
		}
		if json.Unmarshal(rawPaths, &row.ChangedPaths) != nil ||
			len(row.ChangedPaths) < 1 || len(row.ChangedPaths) > 8 {
			return nil, domain.NewError("DOCUMENT_INVALID", 422)
		}
		paths := make(map[string]struct{}, len(row.ChangedPaths))
		for _, path := range row.ChangedPaths {
			switch path {
			case "/ev", "/log_r", "/log_g", "/log_b",
				"/saturation_delta", "/retouch_delta", "/crop", "/selection":
			default:
				return nil, domain.NewError("DOCUMENT_INVALID", 422)
			}
			if _, duplicate := paths[path]; duplicate {
				return nil, domain.NewError("DOCUMENT_INVALID", 422)
			}
			paths[path] = struct{}{}
		}
		if _, duplicate := seen[row.EventID]; duplicate {
			return nil, domain.NewError("DOCUMENT_INVALID", 422)
		}
		seen[row.EventID] = struct{}{}
		out = append(out, row)
	}
	if err = rows.Err(); err != nil {
		return nil, correctionError(err)
	}
	return out, nil
}
