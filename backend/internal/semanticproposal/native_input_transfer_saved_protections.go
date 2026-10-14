package semanticproposal

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// Saved pins are locators only; physical person/protection readers and original
// admission locks remain mandatory. Existing configured pins must match exactly.
func decodeNativeInputTransferProtectionConfiguration(raw []byte, sha string, project uuid.UUID, who auth.Identity, request Request, base TransferPreparation) (TransferPreparation, error) {
	var zero TransferPreparation
	if len(raw) < 2 || len(raw) > 8<<20 {
		return zero, v2input.ErrInvalid
	}
	var config v2input.SemanticDraftConfig
	if json.Unmarshal(raw, &config) != nil || len(config.Semantic.Nodes) < 1 || len(config.Semantic.Nodes) > 5000 {
		return zero, v2input.ErrInvalid
	}
	owned := ownTransferPreparation(base)
	source := config.Semantic.Nodes[0].SourceProtection
	targets := make([]v2input.Ref, len(config.Semantic.Nodes))
	for i, node := range config.Semantic.Nodes {
		if node.SourceProtection != source {
			return zero, v2input.ErrConflict
		}
		targets[i] = node.TargetProtection
	}
	if owned.SourceProtection != (v2input.Ref{}) && owned.SourceProtection != source {
		return zero, v2input.ErrConflict
	}
	if len(owned.TargetProtection) != 0 && !reflect.DeepEqual(owned.TargetProtection, targets) {
		return zero, v2input.ErrConflict
	}
	owned.SourceProtection, owned.TargetProtection = source, targets
	if err := validateTransferProtectionRefs(owned); err != nil {
		return zero, err
	}
	// Full existing schema, scope, exact config hash, ordinal and qualification pin
	// checks still run after extraction. Unknown fields are not silently retained.
	if _, err := decodeNativeInputTransferSavedConfig(raw, sha, project, who, request, owned); err != nil {
		return zero, err
	}
	return owned, nil
}

func (s SQLTransferSourceReader) restoreNativeInputTransferProtectionConfiguration(ctx context.Context, who auth.Identity, request Request, base TransferPreparation) (TransferPreparation, error) {
	var zero TransferPreparation
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	base = ownTransferPreparation(base)
	if err := validateTransferPolicyConfiguration(base); err != nil {
		return zero, err
	}
	if s.DB == nil || s.ProjectID == uuid.Nil || who.UserID != request.ActorID || who.SessionID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	read := func() ([]byte, string, error) {
		var raw []byte
		var sha string
		err := tenant.WithTenant(ctx, s.DB, who, request.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
			return tx.QueryRow(ctx, "SELECT config_snapshot,config_hash FROM photo.semantic_native_input_transfer_saved_config($1,$2,$3,$4,$5)", request.TenantID, s.ProjectID, who.UserID, who.SessionID, request.RunID).Scan(&raw, &sha)
		})
		return raw, sha, err
	}
	raw, sha, err := read()
	if err != nil {
		return zero, err
	}
	var used int64
	if err := chargeMetadata(&used, base.MetadataBytes, len(raw)); err != nil {
		return zero, err
	}
	owned, err := decodeNativeInputTransferProtectionConfiguration(raw, sha, s.ProjectID, who, request, base)
	if err != nil {
		return zero, err
	}
	if err := validateTransferConfiguration(owned); err != nil {
		return zero, err
	}
	after, afterSHA, err := read()
	if err != nil {
		return zero, err
	}
	if afterSHA != sha || string(after) != string(raw) {
		return zero, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return owned, nil
}
