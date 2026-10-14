package v2portrait

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// FrozenRelightTask owns the complete producer inventory plus published Params.
// It grants no SQL shell/transport publication or task admission authority.
type FrozenRelightTask struct {
	draft         v2input.DraftTask
	entries       []v2input.Entry
	reservedBytes int64
}

func freezePreparedRelightTask(ctx context.Context, prepared RelightBootstrapResult, published artifactjson.Ref) (FrozenRelightTask, error) {
	var zero FrozenRelightTask
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	w := prepared.prepared
	if w == nil || prepared.Intent != w.intent || published.ID == uuid.Nil || published.SHA256 != w.intent.ParamsSHA {
		return zero, v2input.ErrInvalid
	}
	id, err := v2input.TaskID(w.request.Workflow, "image.relight", w.request.Ordinal)
	if err != nil || id != w.intent.OwnerID {
		return zero, v2input.ErrInvalid
	}
	node, err := v2input.NodeKey("image.relight", w.request.Ordinal)
	if err != nil || node != w.intent.NodeKey {
		return zero, v2input.ErrInvalid
	}
	var entries []v2input.Entry
	if json.Unmarshal([]byte(w.intent.InputsJCS), &entries) != nil || len(entries) < 10 || len(entries) > 512 {
		return zero, v2input.ErrInvalid
	}
	sha, err := canonical.Hash(entries)
	if err != nil || sha != w.intent.InputsSHA {
		return zero, v2input.ErrInvalid
	}
	for _, entry := range entries {
		if entry.Ref.ArtifactID == published.ID {
			return zero, v2input.ErrInvalid
		}
	}
	entries = append(entries, v2input.Entry{Ref: v2input.Ref{ArtifactID: published.ID, SHA256: published.SHA256}, Contract: "Params"})
	sort.Slice(entries, func(i, j int) bool { return entries[i].Ref.ArtifactID.String() < entries[j].Ref.ArtifactID.String() })
	digest, err := canonical.Hash(struct {
		Task      uuid.UUID       `json:"task"`
		Operation string          `json:"operation"`
		Version   string          `json:"version"`
		Entries   []v2input.Entry `json:"entries"`
	}{id, "image.relight", "1", entries})
	if err != nil {
		return zero, err
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	return FrozenRelightTask{draft: v2input.DraftTask{ID: id, NodeKey: node, Operation: "image.relight", OperationVersion: "1", ResourceClass: "cpu", ParamsArtifactID: published.ID, ParamsSHA: published.SHA256, InputDigest: digest, ExecutionProfile: []byte(w.intent.ProfileJCS)}, entries: entries, reservedBytes: w.intent.ReservedBytes}, nil
}

// PublishPreparedRelightTask binds only the exact private bootstrap inventory.
// Public Documents/Producers buffers are never used to rebuild a smaller plan.
func (s RelightBootstrapCoordinator) PublishPreparedRelightTask(ctx context.Context, objects artifactjson.Objects, prepared RelightBootstrapResult) (FrozenRelightTask, error) {
	ref, err := s.PublishPreparedRelightParams(ctx, objects, prepared)
	if err != nil {
		return FrozenRelightTask{}, err
	}
	return freezePreparedRelightTask(ctx, prepared, ref)
}

func (f FrozenRelightTask) DraftTask() (v2input.DraftTask, error) {
	if f.draft.ID == uuid.Nil || len(f.entries) == 0 {
		return v2input.DraftTask{}, v2input.ErrInvalid
	}
	result := f.draft
	result.ExecutionProfile = append(json.RawMessage(nil), result.ExecutionProfile...)
	return result, nil
}

type relightInputCursor struct {
	entries []v2input.Entry
	index   int
}

func (c *relightInputCursor) Next(ctx context.Context) (v2input.Entry, bool, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Entry{}, false, err
	}
	if c.index == len(c.entries) {
		return v2input.Entry{}, false, nil
	}
	entry := c.entries[c.index]
	c.index++
	return entry, true, nil
}

// OpenInputs returns a fresh cursor over owned compiler entries, without IO.
func (f FrozenRelightTask) OpenInputs(ctx context.Context) (v2input.Cursor, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := f.DraftTask(); err != nil {
		return nil, err
	}
	return &relightInputCursor{entries: append([]v2input.Entry(nil), f.entries...)}, nil
}

// PublishInputClosure requires a committed, authorized blocked shell and its
// scoped transport publisher. Caller must register and admit separately in SQL.
func (f FrozenRelightTask) PublishInputClosure(ctx context.Context, publisher v2input.Publisher) (v2input.GraphTask, error) {
	var zero v2input.GraphTask
	if publisher == nil {
		return zero, v2input.ErrInvalid
	}
	cursor, err := f.OpenInputs(ctx)
	if err != nil {
		return zero, err
	}
	built, err := v2input.BuildInputClosure(ctx, f.draft.ID, cursor, publisher)
	if err != nil {
		return zero, err
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	refs := make([]v2input.Ref, len(f.entries))
	seen := make(map[uuid.UUID]bool, len(f.entries))
	for i, entry := range f.entries {
		refs[i] = entry.Ref
		seen[entry.Ref.ArtifactID] = true
	}
	if seen[built.Manifest.ArtifactID] {
		return zero, v2input.ErrInvalid
	}
	for _, chunk := range built.Chunks {
		if seen[chunk.ArtifactID] {
			return zero, v2input.ErrInvalid
		}
	}
	return v2input.GraphTask{ID: f.draft.ID, Built: built, Open: f.OpenInputs, Direct: refs}, nil
}
