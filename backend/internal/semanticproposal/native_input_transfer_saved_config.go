package semanticproposal

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

type nativeInputTransferSavedMetadata struct {
	legacyQueueOmitted bool
	snapshot       v2input.Ref
	snapshotDigest string
	params         []v2input.Ref
	configSHA      string
}

func decodeNativeInputTransferSavedConfig(raw []byte, sha string, project uuid.UUID, who auth.Identity, request Request, configuration TransferPreparation) (nativeInputTransferSavedMetadata, error) {
	var zero nativeInputTransferSavedMetadata
	if len(raw) < 2 || len(raw) > 8<<20 || project == uuid.Nil || who.UserID != request.ActorID || who.SessionID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	actual, err := canonical.Hash(json.RawMessage(raw))
	if err != nil || actual != sha {
		return zero, v2input.ErrConflict
	}
	var config v2input.SemanticDraftConfig
	if json.Unmarshal(raw, &config) != nil || config.ProjectID != project || config.SnapshotID != request.Intent.TargetSnapshotID || config.Semantic.AuthSessionID != who.SessionID || config.Semantic.Snapshot == nil || config.Semantic.Qualification == nil {
		return zero, v2input.ErrConflict
	}
	if _, err := nativePreparationLegacyQueue(string(raw)); err != nil { return zero, err }
	normalized, err := canonical.Bytes(raw)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	encoded, err := canonical.Marshal(config)
	if err != nil || !bytes.Equal(normalized, encoded) {
		return zero, v2input.ErrInvalid
	}
	if *config.Semantic.Qualification != configuration.Qualification || len(config.Semantic.Nodes) == 0 || len(config.Semantic.Nodes) > 5000 || len(config.Semantic.Nodes) != len(configuration.TargetProtection) {
		return zero, v2input.ErrConflict
	}
	for _, digest := range []string{config.Semantic.Snapshot.SHA256, config.Semantic.SnapshotDigest, sha} {
		if _, err := domain.ParseDigest(digest); err != nil {
			return zero, v2input.ErrInvalid
		}
	}
	if config.Semantic.Snapshot.ArtifactID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	params := make([]v2input.Ref, len(config.Semantic.Nodes))
	seenParams := map[uuid.UUID]bool{}
	for i, node := range config.Semantic.Nodes {
		expected, err := v2input.TaskID(request.WorkflowID, "semantic.transfer", i)
		if err != nil || node.TaskID != expected || node.ParamsArtifactID == uuid.Nil || node.SourceProtection != configuration.SourceProtection || node.TargetProtection != configuration.TargetProtection[i] {
			return zero, v2input.ErrConflict
		}
		if _, err := domain.ParseDigest(node.ParamsSHA); err != nil {
			return zero, v2input.ErrInvalid
		}
		if seenParams[node.ParamsArtifactID] {
			return zero, v2input.ErrConflict
		}
		seenParams[node.ParamsArtifactID] = true
		params[i] = v2input.Ref{ArtifactID: node.ParamsArtifactID, SHA256: node.ParamsSHA}
	}
	return nativeInputTransferSavedMetadata{legacyQueueOmitted: config.QueueClass == "", snapshot: *config.Semantic.Snapshot, snapshotDigest: config.Semantic.SnapshotDigest, params: params, configSHA: sha}, nil
}

func (s SQLTransferSourceReader) readNativeInputTransferSavedConfig(ctx context.Context, who auth.Identity, request Request, configuration TransferPreparation) (nativeInputTransferSavedMetadata, error) {
	var zero nativeInputTransferSavedMetadata
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.ProjectID == uuid.Nil || request.ActorID != who.UserID {
		return zero, v2input.ErrInvalid
	}
	var raw []byte
	var sha string
	err := tenant.WithTenant(ctx, s.DB, who, request.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		return tx.QueryRow(ctx, "SELECT config_snapshot,config_hash FROM photo.semantic_native_input_transfer_saved_config($1,$2,$3,$4,$5)", request.TenantID, s.ProjectID, who.UserID, who.SessionID, request.RunID).Scan(&raw, &sha)
	})
	if err != nil {
		return zero, err
	}
	return decodeNativeInputTransferSavedConfig(raw, sha, s.ProjectID, who, request, configuration)
}
