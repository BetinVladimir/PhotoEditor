package v2portrait

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// ReadRelightRegisteredTransport verifies the exact prepared compiler descriptor
// and reads its committed transport in one transaction. No object IO, quota
// repair, input inserts, graph activation or queue side effects are permitted.
func (s RelightBootstrapCoordinator) ReadRelightRegisteredTransport(ctx context.Context, prepared RelightBootstrapResult, frozen FrozenRelightTask) (v2input.GraphTask, error) {
	var zero v2input.GraphTask
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || prepared.prepared == nil {
		return zero, v2input.ErrInvalid
	}
	draft, err := frozen.DraftTask()
	if err != nil {
		return zero, err
	}
	rebuilt, err := freezePreparedRelightTask(ctx, prepared, artifactjson.Ref{ID: draft.ParamsArtifactID, SHA256: draft.ParamsSHA})
	if err != nil {
		return zero, err
	}
	descriptor, err := frozen.Descriptor()
	if err != nil {
		return zero, err
	}
	expected, err := rebuilt.Descriptor()
	if err != nil || !bytes.Equal(descriptor, expected) {
		return zero, v2input.ErrInvalid
	}
	w := prepared.prepared
	scope := artifactjson.Scope{Tenant: w.request.Tenant, Run: w.request.Run, Owner: w.intent.OwnerID, Purpose: "task_params"}
	proof, err := (relightParamsRepository{coordinator: s, prepared: prepared}).proof(scope, draft.ParamsSHA, int64(len(w.intent.ParamsJCS)))
	if err != nil {
		return zero, err
	}

	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(context.Background())
	if err = s.RevalidatePrepared(ctx, tx, prepared); err != nil {
		return zero, err
	}
	var raw []byte
	if err = tx.QueryRow(ctx, "SELECT photo.read_v28_relight_registered_transport($1,$2,$3,$4,$5::jsonb)", w.request.Tenant, w.request.Run, draft.ID, string(descriptor), proof).Scan(&raw); err != nil {
		return zero, err
	}
	graph, err := restoreRelightRegisteredTransport(ctx, frozen, raw)
	if err != nil {
		return zero, err
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return graph, nil
}

// Structural restoration is not SQL authority.
func restoreRelightRegisteredTransport(ctx context.Context, frozen FrozenRelightTask, raw []byte) (v2input.GraphTask, error) {
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
	if err != nil || wire.Count != len(frozen.entries) || wire.Count < 1 || wire.Count > 513 || wire.SHA != sha || len(wire.Chunks) != (wire.Count+511)/512 || !validRelightTransportRef(wire.Manifest) {
		return zero, v2input.ErrInvalid
	}
	seen := map[uuid.UUID]bool{wire.Manifest.ArtifactID: true}
	for _, chunk := range wire.Chunks {
		if !validRelightTransportRef(chunk) || seen[chunk.ArtifactID] {
			return zero, v2input.ErrInvalid
		}
		seen[chunk.ArtifactID] = true
	}
	entries := append([]v2input.Entry(nil), frozen.entries...)
	direct := make([]v2input.Ref, len(entries))
	for i, e := range entries {
		if seen[e.Ref.ArtifactID] {
			return zero, v2input.ErrInvalid
		}
		direct[i] = e.Ref
	}
	open := func(c context.Context) (v2input.Cursor, error) {
		if err := c.Err(); err != nil {
			return nil, err
		}
		return &relightInputCursor{entries: append([]v2input.Entry(nil), entries...)}, nil
	}
	return v2input.GraphTask{ID: draft.ID, Built: v2input.Built{Manifest: wire.Manifest, Chunks: append([]v2input.Ref(nil), wire.Chunks...), Count: wire.Count, SHA256: wire.SHA}, Direct: direct, Open: open}, nil
}

func validRelightTransportRef(ref v2input.Ref) bool {
	if ref.ArtifactID == uuid.Nil {
		return false
	}
	_, err := domain.ParseDigest(ref.SHA256)
	return err == nil
}
