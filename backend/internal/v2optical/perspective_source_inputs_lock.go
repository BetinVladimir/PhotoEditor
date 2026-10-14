package v2optical

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"sort"
	"time"
)

// LockPerspectiveSourceInputs retains every source edge, even when transport
// deduplicates arrays. Caller owns the transaction and final admission locks.
func LockPerspectiveSourceInputs(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant, project, snapshot uuid.UUID, paramsRaw, sourceRaw []byte, report, calibration v2input.Ref) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(paramsRaw) < 2 || len(paramsRaw) > 65536 || len(sourceRaw) < 2 || len(sourceRaw) > PerspectiveWorkingImageMaxBytes {
		return v2input.ErrInvalid
	}
	paramsRaw = bytes.Clone(paramsRaw)
	sourceRaw = bytes.Clone(sourceRaw)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	params, err := ParsePerspectiveParams(paramsRaw)
	if err != nil {
		return err
	}
	_, edges, err := PerspectiveImageInputs(params, sourceRaw, report, calibration)
	if err != nil {
		return err
	}
	if err = LockPerspectiveSource(ctx, tx, who, tenant, project, snapshot, paramsRaw, sourceRaw); err != nil {
		return err
	}
	base, err := json.Marshal(params.Base)
	if err != nil {
		return err
	}
	if params.Mode == "auto" {
		var locator []byte
		if err := tx.QueryRow(ctx, "SELECT photo.v29_perspective_lines_document($1,$2,$3,$4,$5,$6::jsonb,$7,$8)", tenant, project, snapshot, who.UserID, who.SessionID, string(base), params.Lines.ArtifactID, params.Lines.SHA256).Scan(&locator); err != nil {
			return err
		}
		if len(locator) < 2 || len(locator) > 16384 {
			return v2input.ErrExpired
		}
	}
	sort.SliceStable(edges, func(i, j int) bool {
		return edges[i].Entry.Ref.ArtifactID.String() < edges[j].Entry.Ref.ArtifactID.String()
	})
	for _, edge := range edges {
		if err = ctx.Err(); err != nil {
			return err
		}
		var ready bool
		if err = tx.QueryRow(ctx, "SELECT photo.lock_v29_perspective_source_child($1,$2,$3,$4,$5,$6::jsonb,$7,$8,$9,$10)", tenant, project, snapshot, who.UserID, who.SessionID, string(base), string(sourceRaw), edge.Entry.Ref.ArtifactID, edge.Entry.Ref.SHA256, edge.Role).Scan(&ready); err != nil {
			return err
		}
		if !ready {
			return v2input.ErrExpired
		}
	}
	return ctx.Err()
}
