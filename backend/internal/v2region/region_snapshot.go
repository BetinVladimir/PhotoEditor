package v2region

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// FreezeSnapshotInTransaction retains live source/style authority through freeze.
// Caller owns tenant-local TX and rollback; no object IO occurs here.
func (p FrozenRegionPlan) FreezeSnapshotInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, key string) (RegionSourceSnapshot, error) {
	if tx == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return RegionSourceSnapshot{}, ErrRegionInvalid
	}
	document, err := p.decode()
	if err != nil {
		return RegionSourceSnapshot{}, err
	}
	run, err := v2input.RunID(document.TenantID, who.UserID, key)
	if err != nil {
		return RegionSourceSnapshot{}, err
	}
	snapshot, err := p.SourceSnapshot(run)
	if err != nil {
		return RegionSourceSnapshot{}, err
	}
	var frozen bool
	err = tx.QueryRow(ctx, "SELECT photo.freeze_v26_region_snapshot($1,$2,$3,$4,$5,$6,$7)", document.TenantID, run, who.UserID, who.SessionID, json.RawMessage(p.JSON()), string(snapshot.JSON()), snapshot.SHA256).Scan(&frozen)
	if err != nil {
		return RegionSourceSnapshot{}, err
	}
	if !frozen {
		return RegionSourceSnapshot{}, ErrRegionInvalid
	}
	return snapshot, nil
}

// RegionSourceSnapshot is an internal snapshot descriptor, not an uploaded
// worker contract. Persistence must repeat source authority and freeze its row.
type RegionSourceSnapshot struct {
	ID      uuid.UUID
	SHA256  string
	body    []byte
	binding v2input.SessionVersionBinding
}

func (s RegionSourceSnapshot) JSON() []byte                           { return append([]byte(nil), s.body...) }
func (s RegionSourceSnapshot) Binding() v2input.SessionVersionBinding { return s.binding }

// SourceSnapshot binds one run to one source version/canvas. ROI and recipe
// edits do not change source identity. The descriptor cannot authorize SQL IO.
func (p FrozenRegionPlan) SourceSnapshot(run uuid.UUID) (RegionSourceSnapshot, error) {
	if run == uuid.Nil {
		return RegionSourceSnapshot{}, ErrRegionInvalid
	}
	document, err := p.decode()
	if err != nil {
		return RegionSourceSnapshot{}, err
	}
	id := uuid.NewSHA1(run, []byte("region.snapshot"))
	binding := document.FrozenRecipe.Source
	value := struct {
		Schema  int                           `json:"schema_version"`
		ID      uuid.UUID                     `json:"snapshot_id"`
		Binding v2input.SessionVersionBinding `json:"binding"`
	}{1, id, binding}
	body, err := canonical.Marshal(value)
	if err != nil {
		return RegionSourceSnapshot{}, err
	}
	digest, err := canonical.Hash(value)
	if err != nil {
		return RegionSourceSnapshot{}, err
	}
	return RegionSourceSnapshot{id, digest, body, binding}, nil
}
