package v2learning

import (
	"context"
	"sort"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// RevalidateTrainingManifest holds the tenant/profile locks acquired by the
// candidate source until the caller commits its freeze transaction. It rejects
// revoked consent and changes to the exact approved recipe or source lineage.
func RevalidateTrainingManifest(ctx context.Context, tx pgx.Tx,
	tenantID, actorID, authSessionID uuid.UUID, expectedRevision int64,
	manifest TrainingManifest) error {
	if tx == nil || manifest.ProfileID == uuid.Nil ||
		len(manifest.Examples) < 40 || len(manifest.Examples) > 5000 {
		return ErrInvalidTrainingManifest
	}
	sessionSet := make(map[uuid.UUID]struct{})
	for _, item := range manifest.Examples {
		if item.SessionID == uuid.Nil {
			return ErrInvalidTrainingManifest
		}
		sessionSet[item.SessionID] = struct{}{}
	}
	if len(sessionSet) < 6 || len(sessionSet) > 5000 {
		return ErrInvalidTrainingManifest
	}
	sessions := make([]uuid.UUID, 0, len(sessionSet))
	for id := range sessionSet {
		sessions = append(sessions, id)
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].String() < sessions[j].String() })
	rows, err := LoadTrainingCandidates(ctx, tx, tenantID, manifest.ProfileID,
		expectedRevision, sessions, actorID, authSessionID)
	if err != nil {
		return err
	}
	return MatchTrainingManifestCandidates(manifest, rows)
}

// MatchTrainingManifestCandidates is the exact relational comparison used by
// the freeze transaction after querying current correction/approval/grant rows.
// The training split may intentionally exclude otherwise eligible events.
func MatchTrainingManifestCandidates(manifest TrainingManifest,
	rows []TrainingCandidate) error {
	if manifest.SchemaVersion != 1 || manifest.ProfileID == uuid.Nil ||
		len(manifest.Examples) < 40 || len(manifest.Examples) > 5000 {
		return ErrInvalidTrainingManifest
	}
	byEvent := make(map[uuid.UUID]TrainingCandidate, len(rows))
	for _, row := range rows {
		if row.EventID == uuid.Nil {
			return ErrInvalidTrainingManifest
		}
		if _, duplicate := byEvent[row.EventID]; duplicate {
			return ErrInvalidTrainingManifest
		}
		byEvent[row.EventID] = row
	}
	seen := make(map[uuid.UUID]struct{}, len(manifest.Examples))
	consents := make(map[uuid.UUID]int64)
	for _, example := range manifest.Examples {
		if example.EventID == uuid.Nil || example.SessionID == uuid.Nil {
			return ErrInvalidTrainingManifest
		}
		if _, duplicate := seen[example.EventID]; duplicate {
			return ErrInvalidTrainingManifest
		}
		seen[example.EventID] = struct{}{}
		row, found := byEvent[example.EventID]
		if !found || !row.ConsentLive || !row.SourceReady || !row.BaseReady ||
			!row.ApprovedReady || row.ApprovalID == nil ||
			row.SessionID != example.SessionID ||
			row.AssetVersionID != example.RenderCase.Binding.AssetVersionID ||
			row.SourceLineageSHA != example.RenderCase.SourceLineageSHA ||
			*row.ApprovalID != example.RenderCase.ApprovalID ||
			!validManifestRef(example.RenderCase.BaseRecipe) ||
			!validManifestRef(example.RenderCase.ApprovedRecipe) ||
			!validManifestRef(example.RenderCase.DescriptorShard) ||
			example.RenderCase.DescriptorAnalysisID == uuid.Nil ||
			example.RenderCase.DescriptorOrdinal < 0 ||
			example.RenderCase.DescriptorOrdinal > 4999 ||
			row.BaseArtifactID != example.RenderCase.BaseDocument.ArtifactID ||
			row.BaseSHA != example.RenderCase.BaseDocument.SHA256 ||
			row.ApprovedArtifactID != example.RenderCase.ApprovedDocument.ArtifactID ||
			row.ApprovedSHA != example.RenderCase.ApprovedDocument.SHA256 ||
			row.ConsentID == uuid.Nil || row.ConsentEpoch < 1 {
			return ErrInvalidTrainingManifest
		}
		for _, path := range row.ChangedPaths {
			switch path {
			case "/ev", "/log_r", "/log_g", "/log_b",
				"/saturation_delta", "/retouch_delta":
			default:
				return ErrInvalidTrainingManifest
			}
		}
		if epoch, exists := consents[row.ConsentID]; exists && epoch != row.ConsentEpoch {
			return ErrInvalidTrainingManifest
		}
		consents[row.ConsentID] = row.ConsentEpoch
	}
	if len(consents) != len(manifest.Consents) {
		return ErrInvalidTrainingManifest
	}
	for _, consent := range manifest.Consents {
		epoch, found := consents[consent.ConsentID]
		if !found || consent.Epoch != strconv.FormatInt(epoch, 10) {
			return ErrInvalidTrainingManifest
		}
		delete(consents, consent.ConsentID)
	}
	if len(consents) != 0 {
		return ErrInvalidTrainingManifest
	}
	return nil
}
