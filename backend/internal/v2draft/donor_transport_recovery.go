package v2draft

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"time"
)

// ReadRegisteredTransport revalidates the exact qualified descriptor and reads
// its retained manifest/chunk without object IO or input inserts. Only a committed
// matching closure is returned. Final graph admission must repeat live authority.
func (s SQLDonorDescriptorStore) ReadRegisteredTransport(ctx context.Context, who auth.Identity, tenant, run uuid.UUID, frozen FrozenDonorTask) (v2input.GraphTask, error) {
	var zero v2input.GraphTask
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || tenant == uuid.Nil || run == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || frozen.qualification == nil {
		return zero, v2input.ErrInvalid
	}
	if _, err := frozen.qualification.Qualification(); err != nil {
		return zero, err
	}
	descriptor, err := frozen.Descriptor()
	if err != nil {
		return zero, err
	}
	draft, err := frozen.DraftTask()
	if err != nil {
		return zero, err
	}
	params := append([]byte(nil), frozen.params...)
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := s.DB.Begin(bounded)
	if err != nil {
		return zero, err
	}
	if tx == nil {
		return zero, v2input.ErrInvalid
	}
	defer rollbackDonorRecovery(bounded, tx)
	if _, err = tx.Exec(bounded, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return zero, err
	}
	var role string
	if err = tx.QueryRow(bounded, "SELECT current_user").Scan(&role); err != nil {
		return zero, err
	}
	if role != "photo_scheduler" {
		return zero, v2input.ErrInvalid
	}
	var ready bool
	if err = tx.QueryRow(bounded, "SELECT photo.stage_v28_donor_task($1,$2,$3,$4,$5,$6)", tenant, run, who.UserID, who.SessionID, string(descriptor), string(params)).Scan(&ready); err != nil {
		return zero, err
	}
	if !ready {
		return zero, v2input.ErrExpired
	}
	var raw []byte
	if err = tx.QueryRow(bounded, "SELECT photo.read_v28_donor_registered_transport($1,$2,$3,$4,$5)", tenant, run, draft.ID, who.UserID, who.SessionID).Scan(&raw); err != nil {
		return zero, err
	}
	graph, err := restoreDonorRegisteredTransport(bounded, frozen, raw)
	if err != nil {
		return zero, err
	}
	if err = bounded.Err(); err != nil {
		return zero, err
	}
	if err = tx.Commit(bounded); err != nil {
		return zero, err
	}
	return graph, nil
}

// Pure structural restoration; SQL authority is established separately above.
func restoreDonorRegisteredTransport(ctx context.Context, frozen FrozenDonorTask, raw []byte) (v2input.GraphTask, error) {
	var zero v2input.GraphTask
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if len(raw) < 2 || len(raw) > 65536 {
		return zero, v2input.ErrExpired
	}
	normal, err := canonical.Bytes(raw)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	var wire struct {
		Manifest v2input.Ref   `json:"manifest"`
		Chunks   []v2input.Ref `json:"chunks"`
		Count    int           `json:"count"`
		SHA      string        `json:"sha256"`
	}
	if json.Unmarshal(normal, &wire) != nil {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(wire)
	if err != nil || !bytes.Equal(exact, normal) {
		return zero, v2input.ErrInvalid
	}
	draft, err := frozen.DraftTask()
	if err != nil {
		return zero, err
	}
	sha, err := canonical.Hash(frozen.entries)
	if err != nil || wire.Count != len(frozen.entries) || wire.Count < 1 || wire.Count > 127 || wire.SHA != sha || len(wire.Chunks) != 1 || !isDocumentRef(wire.Manifest) || !isDocumentRef(wire.Chunks[0]) || wire.Manifest.ArtifactID == wire.Chunks[0].ArtifactID {
		return zero, v2input.ErrInvalid
	}
	entries := append([]v2input.Entry(nil), frozen.entries...)
	direct := make([]v2input.Ref, len(entries))
	for i, e := range entries {
		if e.Ref.ArtifactID == wire.Manifest.ArtifactID || e.Ref.ArtifactID == wire.Chunks[0].ArtifactID {
			return zero, v2input.ErrInvalid
		}
		direct[i] = e.Ref
	}
	open := func(c context.Context) (v2input.Cursor, error) {
		if err := c.Err(); err != nil {
			return nil, err
		}
		return &donorInputCursor{entries: append([]v2input.Entry(nil), entries...)}, nil
	}
	return v2input.GraphTask{ID: draft.ID, Built: v2input.Built{Manifest: wire.Manifest, Chunks: append([]v2input.Ref(nil), wire.Chunks...), Count: wire.Count, SHA256: wire.SHA}, Direct: direct, Open: open}, nil
}
