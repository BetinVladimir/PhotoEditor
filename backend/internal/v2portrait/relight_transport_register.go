package v2portrait

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// RegisterRelightTransport ignores caller Open and registers only the owned
// frozen inventory. Live authority, exact transport and input sealing share one
// transaction; graph admission/queue remains a separate operation.
func (s RelightBootstrapCoordinator) RegisterRelightTransport(ctx context.Context, prepared RelightBootstrapResult, frozen FrozenRelightTask, graph v2input.GraphTask) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.DB == nil || prepared.prepared == nil {
		return v2input.ErrInvalid
	}
	draft, err := frozen.DraftTask()
	if err != nil {
		return err
	}
	rebuilt, err := freezePreparedRelightTask(ctx, prepared, artifactjson.Ref{ID: draft.ParamsArtifactID, SHA256: draft.ParamsSHA})
	if err != nil {
		return err
	}
	descriptor, err := frozen.Descriptor()
	if err != nil {
		return err
	}
	expected, err := rebuilt.Descriptor()
	if err != nil || !bytes.Equal(descriptor, expected) {
		return v2input.ErrInvalid
	}
	sha, err := canonical.Hash(frozen.entries)
	if err != nil || graph.ID != draft.ID || graph.Built.Count != len(frozen.entries) || graph.Built.SHA256 != sha || len(graph.Built.Chunks) != (len(frozen.entries)+511)/512 || len(graph.Direct) != len(frozen.entries) {
		return v2input.ErrInvalid
	}
	seen := make(map[uuid.UUID]bool, len(frozen.entries)+3)
	for i, entry := range frozen.entries {
		if graph.Direct[i] != entry.Ref {
			return v2input.ErrInvalid
		}
		seen[entry.Ref.ArtifactID] = true
	}
	refs := append([]v2input.Ref(nil), graph.Built.Chunks...)
	refs = append(refs, graph.Built.Manifest)
	for _, ref := range refs {
		if ref.ArtifactID == uuid.Nil || seen[ref.ArtifactID] {
			return v2input.ErrInvalid
		}
		if _, err = domain.ParseDigest(ref.SHA256); err != nil {
			return v2input.ErrInvalid
		}
		seen[ref.ArtifactID] = true
	}
	manifest, err := canonical.Marshal(v2input.Manifest{SchemaVersion: 1, TaskID: draft.ID, EntryCount: len(frozen.entries), EntriesSHA: sha, Chunks: graph.Built.Chunks})
	if err != nil {
		return err
	}
	transport, err := canonical.Marshal(struct {
		Manifest v2input.Ref   `json:"manifest"`
		Chunks   []v2input.Ref `json:"chunks"`
		Count    int           `json:"count"`
		SHA256   string        `json:"sha256"`
	}{graph.Built.Manifest, graph.Built.Chunks, len(frozen.entries), sha})
	if err != nil {
		return err
	}
	w := prepared.prepared
	proof, err := (relightParamsRepository{coordinator: s, prepared: prepared}).proof(artifactjson.Scope{Tenant: w.request.Tenant, Run: w.request.Run, Owner: w.intent.OwnerID, Purpose: "task_params"}, w.intent.ParamsSHA, int64(len(w.intent.ParamsJCS)))
	if err != nil {
		return err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if err = s.RevalidatePrepared(ctx, tx, prepared); err != nil {
		return err
	}
	var ready bool
	if err = tx.QueryRow(ctx, "SELECT photo.register_v28_relight_transport($1,$2,$3,$4::jsonb,$5::jsonb,$6)", w.request.Tenant, w.request.Run, string(descriptor), proof, string(transport), string(manifest)).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrExpired
	}
	return tx.Commit(ctx)
}
