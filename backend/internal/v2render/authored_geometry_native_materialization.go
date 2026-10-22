package v2render

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

// AuthoredNativeMaterialization is private physical/current-source evidence, not
// an admitted native proposal or a caller-selected replacement source.
type AuthoredNativeMaterialization struct {
	source   AuthoredInstructionSource
	selected AuthoredGeometryMaterialization
	authored []byte
	proof    []byte
}
type SQLAuthoredNativeMaterializer struct {
	Scheduler *pgxpool.Pool
	Objects   SQLAuthoredGeometryPhaseObjects
}

func (s SQLAuthoredNativeMaterializer) Read(ctx context.Context, who auth.Identity, tenant, parent uuid.UUID, ordinal int) (AuthoredNativeMaterialization, error) {
	var zero AuthoredNativeMaterialization
	if e := ctx.Err(); e != nil {
		return zero, e
	}
	if s.Scheduler == nil || s.Objects.API == nil || s.Objects.Objects == nil || s.Objects.Bucket == "" {
		return zero, v2input.ErrInvalid
	}
	source, found, e := ReadAuthoredInstructionSource(ctx, s.Scheduler, tenant, parent, ordinal)
	if e != nil {
		return zero, e
	}
	if !found {
		return zero, v2input.ErrExpired
	}
	if source.Actor != who.UserID || source.Session != who.SessionID || source.Instruction.Kind != "geometry" || source.Instruction.GeometryLayer == nil {
		return zero, v2input.ErrInvalid
	}
	var stack struct {
		Layers []struct {
			ID     uuid.UUID   `json:"id"`
			Params v2input.Ref `json:"params"`
		}
	}
	if json.Unmarshal([]byte(source.LayerStackJCS), &stack) != nil {
		return zero, v2input.ErrInvalid
	}
	var selected v2input.Ref
	count := 0
	for _, layer := range stack.Layers {
		if layer.ID == *source.Instruction.GeometryLayer {
			count++
			selected = layer.Params
		}
	}
	if count != 1 || !validLayerArtifactRef(selected) {
		return zero, v2input.ErrInvalid
	}
	authored, e := s.Objects.ReadJSON(ctx, who, tenant, source.Project, parent, ordinal, selected)
	if e != nil {
		return zero, e
	}
	materialized, e := MaterializeAuthoredInstruction(source, authored)
	if e != nil {
		return zero, e
	}
	switch materialized.Operation {
	case "image.upscale", "optical.panorama", "optical.focus_stack":
	default:
		return zero, v2input.ErrInvalid
	}
	read := func() ([]byte, error) {
		var proof []byte
		e := database.TenantTransaction(ctx, s.Scheduler, tenant, func(tx pgx.Tx) error {
			var role string
			if e := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); e != nil {
				return e
			}
			if role != "photo_scheduler" {
				return v2input.ErrInvalid
			}
			return tx.QueryRow(ctx, `SELECT photo.authored_render_native_materialization_current($1,$2,$3,$4,$5,$6,$7,$8)`, tenant, parent, ordinal, who.UserID, who.SessionID, materialized.Operation, string(authored), string(materialized.Params.Body)).Scan(&proof)
		})
		return proof, e
	}
	before, e := read()
	if e != nil {
		return zero, e
	}
	if len(before) < 2 || bytes.Equal(before, []byte("null")) {
		return zero, v2input.ErrExpired
	}
	expected, e := canonical.Marshal(source)
	actual, e2 := canonical.Bytes(before)
	if e != nil || e2 != nil || !bytes.Equal(expected, actual) {
		return zero, v2input.ErrExpired
	}
	// Reopen the exact immutable original occurrence after SQL authority. A
	// changed version or withdrawn current prefix must never become native Ready.
	again, e := s.Objects.ReadJSON(ctx, who, tenant, source.Project, parent, ordinal, selected)
	if e != nil {
		return zero, e
	}
	if !bytes.Equal(authored, again) {
		return zero, v2input.ErrExpired
	}
	after, e := read()
	if e != nil {
		return zero, e
	}
	if !bytes.Equal(before, after) {
		return zero, v2input.ErrExpired
	}
	if e = ctx.Err(); e != nil {
		return zero, e
	}
	return AuthoredNativeMaterialization{source: source, selected: materialized, authored: append([]byte(nil), authored...), proof: append([]byte(nil), before...)}, nil
}
