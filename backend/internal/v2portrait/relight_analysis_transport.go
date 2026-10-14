package v2portrait

import (
	"context"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type relightAnalysisCursor struct {
	entries []v2input.Entry
	next    int
}

func (c *relightAnalysisCursor) Next(ctx context.Context) (v2input.Entry, bool, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Entry{}, false, err
	}
	if c.next == len(c.entries) {
		return v2input.Entry{}, false, nil
	}
	entry := c.entries[c.next]
	c.next++
	return entry, true, nil
}

// PublishInputClosure only accepts a trusted scoped publisher installed after
// dedicated SQL staging. It does not grant storage, registration or admission
// authority to the supplied publisher or returned GraphTask.
func (p RelightAnalysisPlan) PublishInputClosure(ctx context.Context, publisher v2input.Publisher) (v2input.GraphTask, error) {
	if _, err := p.DraftTask(ctx); err != nil {
		return v2input.GraphTask{}, err
	}
	if publisher == nil {
		return v2input.GraphTask{}, v2input.ErrInvalid
	}
	s := p.Snapshot()
	built, err := v2input.BuildInputClosure(ctx, s.Task, &relightAnalysisCursor{entries: s.Inputs}, publisher)
	if err != nil {
		return v2input.GraphTask{}, err
	}
	return p.BindInputClosure(ctx, built)
}

// BindInputClosure checks exact inventory/manifest content for recovery. Object
// existence and current source authority must still be checked in SQL.
func (p RelightAnalysisPlan) BindInputClosure(ctx context.Context, built v2input.Built) (v2input.GraphTask, error) {
	var zero v2input.GraphTask
	if _, err := p.DraftTask(ctx); err != nil {
		return zero, err
	}
	s := p.Snapshot()
	entriesSHA, err := canonical.Hash(s.Inputs)
	if err != nil || built.SHA256 != entriesSHA || built.Count != len(s.Inputs) || len(built.Chunks) != 1 || !validRelightTransportRef(built.Manifest) {
		return zero, v2input.ErrInvalid
	}
	chunkSHA, err := canonical.Hash(v2input.Chunk{SchemaVersion: 1, Entries: s.Inputs})
	if err != nil || !validRelightTransportRef(built.Chunks[0]) || built.Chunks[0].SHA256 != chunkSHA {
		return zero, v2input.ErrInvalid
	}
	manifestSHA, err := canonical.Hash(v2input.Manifest{SchemaVersion: 1, TaskID: s.Task, EntryCount: len(s.Inputs), EntriesSHA: entriesSHA, Chunks: built.Chunks})
	if err != nil || built.Manifest.SHA256 != manifestSHA || built.Manifest.ArtifactID == built.Chunks[0].ArtifactID {
		return zero, v2input.ErrInvalid
	}
	direct := make([]v2input.Ref, len(s.Inputs))
	for i, entry := range s.Inputs {
		if entry.Ref.ArtifactID == built.Manifest.ArtifactID || entry.Ref.ArtifactID == built.Chunks[0].ArtifactID {
			return zero, v2input.ErrInvalid
		}
		direct[i] = entry.Ref
	}
	owned := built
	owned.Chunks = append([]v2input.Ref(nil), built.Chunks...)
	return v2input.GraphTask{ID: s.Task, Built: owned, Direct: direct, Open: func(ctx context.Context) (v2input.Cursor, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return &relightAnalysisCursor{entries: append([]v2input.Entry(nil), s.Inputs...)}, nil
	}}, nil
}
