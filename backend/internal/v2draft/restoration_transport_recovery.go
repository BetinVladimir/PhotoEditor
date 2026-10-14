package v2draft

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type restorationRegisteredTransportWire struct {
	Manifest v2input.Ref   `json:"manifest"`
	Chunks   []v2input.Ref `json:"chunks"`
	Count    int           `json:"count"`
	SHA      string        `json:"sha256"`
}

// ReadRegisteredTransport recovers only a previously committed exact input
// closure. SQL rechecks immutable versions and current descriptor authority;
// it performs no object writes or new input registrations.
func (s SQLRestorationDescriptorStore) ReadRegisteredTransport(ctx context.Context,
	who auth.Identity, tenant, run uuid.UUID, prepared RestorationResolvedDraft,
	paramsRaw []byte) (v2input.GraphTask, error) {
	var zero v2input.GraphTask
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || tenant == uuid.Nil || run == uuid.Nil ||
		who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	descriptor, err := prepared.Descriptor(paramsRaw)
	if err != nil {
		return zero, err
	}
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
	if err = tx.QueryRow(bounded, "SELECT photo.stage_v28_restoration_task($1,$2,$3,$4,$5,$6)",
		tenant, run, who.UserID, who.SessionID, string(descriptor), string(paramsRaw)).Scan(&ready); err != nil {
		return zero, err
	}
	if !ready {
		return zero, v2input.ErrExpired
	}
	var raw []byte
	if err = tx.QueryRow(bounded, "SELECT photo.read_v28_restoration_registered_transport($1,$2,$3,$4,$5)",
		tenant, run, prepared.Draft.Task.ID, who.UserID, who.SessionID).Scan(&raw); err != nil {
		return zero, err
	}
	graph, err := restoreRestorationRegisteredTransport(bounded, prepared, raw)
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

// Pure transport reconstruction follows independent SQL authority and only
// accepts the descriptor's own frozen inventory.
func restoreRestorationRegisteredTransport(ctx context.Context,
	prepared RestorationResolvedDraft, raw []byte) (v2input.GraphTask, error) {
	var zero v2input.GraphTask
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if len(raw) < 2 || len(raw) > 65536 {
		return zero, v2input.ErrExpired
	}
	normal, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normal, raw) {
		return zero, v2input.ErrInvalid
	}
	var wire restorationRegisteredTransportWire
	if json.Unmarshal(raw, &wire) != nil {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(wire)
	if err != nil || !bytes.Equal(exact, raw) {
		return zero, v2input.ErrInvalid
	}
	entries := append([]v2input.Entry(nil), prepared.Draft.Entries...)
	sha, err := canonical.Hash(entries)
	if err != nil || wire.Count != len(entries) || wire.Count < 6 || wire.Count > 127 ||
		wire.SHA != sha || len(wire.Chunks) != 1 || !isDocumentRef(wire.Manifest) ||
		!isDocumentRef(wire.Chunks[0]) || wire.Manifest.ArtifactID == wire.Chunks[0].ArtifactID {
		return zero, v2input.ErrInvalid
	}
	direct := make([]v2input.Ref, len(entries))
	for i, entry := range entries {
		if !isDocumentRef(entry.Ref) || entry.Ref.ArtifactID == wire.Manifest.ArtifactID ||
			entry.Ref.ArtifactID == wire.Chunks[0].ArtifactID {
			return zero, v2input.ErrInvalid
		}
		direct[i] = entry.Ref
	}
	open := func(c context.Context) (v2input.Cursor, error) {
		if err := c.Err(); err != nil {
			return nil, err
		}
		return &donorInputCursor{entries: append([]v2input.Entry(nil), entries...)}, nil
	}
	return v2input.GraphTask{ID: prepared.Draft.Task.ID,
		Built: v2input.Built{Manifest: wire.Manifest, Chunks: append([]v2input.Ref(nil), wire.Chunks...),
			Count: wire.Count, SHA256: wire.SHA}, Direct: direct, Open: open}, nil
}
