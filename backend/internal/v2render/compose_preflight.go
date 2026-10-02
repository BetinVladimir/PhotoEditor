package v2render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

type SQLOriginalCompositionPreflight struct{ DB *pgxpool.Pool }

// Check connects the original-source metadata and computed resource predicates.
// It is a read-only scheduler preflight, not task admission or object-byte proof.
// Atomic shell/registration must repeat these checks with its frozen artifacts.
func (s SQLOriginalCompositionPreflight) Check(ctx context.Context, tenant, project, snapshot uuid.UUID, who auth.Identity, params json.RawMessage, width, height int64, profile json.RawMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.DB == nil || tenant == uuid.Nil || project == uuid.Nil || snapshot == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || width < 1 || height < 1 || width > 200000000 || height > 200000000/width || len(params) < 2 || len(params) > 1<<20 || len(profile) < 2 || len(profile) > 65536 {
		return v2input.ErrInvalid
	}
	paramsJCS, err := canonical.Bytes(params)
	if err != nil || !bytes.Equal(paramsJCS, params) {
		return v2input.ErrInvalid
	}
	profileJCS, err := canonical.Bytes(profile)
	if err != nil || !bytes.Equal(profileJCS, profile) {
		return v2input.ErrInvalid
	}
	return database.InTransaction(ctx, s.DB, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_scheduler" {
			return v2input.ErrInvalid
		}
		var accepted bool
		if err := tx.QueryRow(ctx, `SELECT photo.v25_layer_original_preflight($1,$2,$3,$4,$5,$6::jsonb,$7,$8,$9::jsonb)`, tenant, project, snapshot, who.UserID, who.SessionID, json.RawMessage(paramsJCS), width, height, json.RawMessage(profileJCS)).Scan(&accepted); err != nil {
			return err
		}
		if !accepted {
			return v2input.ErrExpired
		}
		return nil
	})
}

// CheckParams additionally binds exact frozen bytes to a completed version-
// scoped params upload. The second transaction repeats live preflight, so an
// authority change after Check cannot authorize the artifact through a cache.
func (s SQLOriginalCompositionPreflight) CheckParams(ctx context.Context, tenant, project, snapshot uuid.UUID, who auth.Identity, artifact v2input.Ref, params json.RawMessage, width, height int64, profile json.RawMessage) error {
	if !validLayerArtifactRef(artifact) {
		return v2input.ErrInvalid
	}
	sum := sha256.Sum256(params)
	if hex.EncodeToString(sum[:]) != artifact.SHA256 {
		return v2input.ErrInvalid
	}
	if err := s.Check(ctx, tenant, project, snapshot, who, params, width, height, profile); err != nil {
		return err
	}
	return database.InTransaction(ctx, s.DB, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_scheduler" {
			return v2input.ErrInvalid
		}
		var accepted bool
		if err := tx.QueryRow(ctx, `SELECT photo.v25_layer_original_params_scope($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb)`, tenant, project, snapshot, who.UserID, who.SessionID, artifact.ArtifactID, artifact.SHA256, string(params), width, height, json.RawMessage(profile)).Scan(&accepted); err != nil {
			return err
		}
		if !accepted {
			return v2input.ErrExpired
		}
		return nil
	})
}

// CheckTaskParams also proves that params were published for this task's stable
// document ID. SQL repeats live artifact/source scope in this last transaction.
// Task UUID derivation and proposal/mask authority remain admission checks.
func (s SQLOriginalCompositionPreflight) CheckTaskParams(ctx context.Context, tenant, project, snapshot uuid.UUID, who auth.Identity, task uuid.UUID, artifact v2input.Ref, params json.RawMessage, width, height int64, profile json.RawMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if task == uuid.Nil {
		return v2input.ErrInvalid
	}
	if err := s.CheckParams(ctx, tenant, project, snapshot, who, artifact, params, width, height, profile); err != nil {
		return err
	}
	return database.InTransaction(ctx, s.DB, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_scheduler" {
			return v2input.ErrInvalid
		}
		var accepted bool
		if err := tx.QueryRow(ctx, `SELECT photo.v25_layer_original_task_params_scope($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::jsonb)`, tenant, project, snapshot, who.UserID, who.SessionID, task, artifact.ArtifactID, artifact.SHA256, string(params), width, height, json.RawMessage(profile)).Scan(&accepted); err != nil {
			return err
		}
		if !accepted {
			return v2input.ErrExpired
		}
		return nil
	})
}
