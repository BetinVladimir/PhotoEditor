package semanticproposal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// Preserve a historical immutable config only when it is exactly the rebuilt
// config with the newly introduced locator omitted. No other field may drift.
func compatibleDefectConfig(current, saved []byte, savedSHA string) ([]byte, error) {
	if len(current) < 2 || len(current) > 1<<20 || len(saved) < 2 || len(saved) > 1<<20 {
		return nil, v2input.ErrInvalid
	}
	actual, err := canonical.Bytes(saved)
	if err != nil {
		return nil, v2input.ErrInvalid
	}
	sha, err := canonical.Hash(json.RawMessage(actual))
	if err != nil || sha != savedSHA {
		return nil, v2input.ErrConflict
	}
	expected, err := canonical.Bytes(current)
	if err != nil {
		return nil, v2input.ErrInvalid
	}
	if bytes.Equal(actual, expected) {
		return actual, nil
	}
	var config map[string]json.RawMessage
	if json.Unmarshal(expected, &config) != nil {
		return nil, v2input.ErrInvalid
	}
	var native map[string]json.RawMessage
	if json.Unmarshal(config["v27_defect_prepare"], &native) != nil || native["original_producer"] == nil {
		return nil, v2input.ErrInvalid
	}
	delete(native, "original_producer")
	config["v27_defect_prepare"], err = canonical.Marshal(native)
	if err != nil {
		return nil, v2input.ErrInvalid
	}
	legacy, err := canonical.Marshal(config)
	if err != nil {
		return nil, v2input.ErrInvalid
	}
	if !bytes.Equal(actual, legacy) {
		return nil, v2input.ErrConflict
	}
	return actual, nil
}

func (s SQLTransferSourceReader) preserveDefectConfig(ctx context.Context, who auth.Identity, tenantID, run uuid.UUID, current []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.DB == nil || s.ProjectID == uuid.Nil || tenantID == uuid.Nil || run == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return nil, v2input.ErrInvalid
	}
	var saved []byte
	var sha string
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		return tx.QueryRow(ctx, `SELECT config_snapshot,config_hash FROM photo.semantic_defect_saved_config($1,$2,$3,$4,$5)`, tenantID, s.ProjectID, who.UserID, who.SessionID, run).Scan(&saved, &sha)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return append([]byte(nil), current...), ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	return compatibleDefectConfig(current, saved, sha)
}
