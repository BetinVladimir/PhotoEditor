package v2optical

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

type negativeEntryCursor struct {
	entries []v2input.Entry
	index   int
}

func (c *negativeEntryCursor) Next(ctx context.Context) (v2input.Entry, bool, error) {
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

// BuildNegativeTaskTransport publishes bounded chunks/manifest through the
// existing protocol. Publisher must enforce the saved draft owner. Caller must
// repeat live source/qualification/resource admission before final graph seal.
func BuildNegativeTaskTransport(ctx context.Context, task uuid.UUID, plan NegativeTaskPlan, sourceRaw []byte, qualified VerifiedNegativeQualification, params v2input.Ref, publisher v2input.Publisher) (v2input.GraphTask, error) {
	if task == uuid.Nil || publisher == nil {
		return v2input.GraphTask{}, v2input.ErrInvalid
	}
	entries, direct, err := NegativeTaskInputEntries(ctx, plan, sourceRaw, qualified, params)
	if err != nil {
		return v2input.GraphTask{}, err
	}
	frozen := append([]v2input.Entry(nil), entries...)
	open := func(ctx context.Context) (v2input.Cursor, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return &negativeEntryCursor{entries: frozen}, nil
	}
	cursor, err := open(ctx)
	if err != nil {
		return v2input.GraphTask{}, err
	}
	built, err := v2input.BuildInputClosure(ctx, task, cursor, publisher)
	if err != nil {
		return v2input.GraphTask{}, err
	}
	inputs := make(map[uuid.UUID]struct{}, len(frozen))
	for _, entry := range frozen {
		inputs[entry.Ref.ArtifactID] = struct{}{}
	}
	refs := append([]v2input.Ref{built.Manifest}, built.Chunks...)
	for _, ref := range refs {
		if _, exists := inputs[ref.ArtifactID]; exists {
			return v2input.GraphTask{}, v2input.ErrInvalid
		}
	}
	if err := ctx.Err(); err != nil {
		return v2input.GraphTask{}, err
	}
	return v2input.GraphTask{ID: task, Built: built, Open: open, Direct: append([]v2input.Ref(nil), direct...)}, nil
}
