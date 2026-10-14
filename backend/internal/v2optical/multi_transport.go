package v2optical

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// BuildMultiTaskTransport publishes bounded chunks/manifest through the
// existing protocol. Publisher must enforce the saved draft owner. Caller must
// repeat live source/qualification/resource admission before final graph seal.
func BuildMultiTaskTransport(ctx context.Context, task uuid.UUID, plan MultiTaskPlan, originals [][]byte, qualified VerifiedMultiQualification, params v2input.Ref, publisher v2input.Publisher) (v2input.GraphTask, error) {
	if task == uuid.Nil || publisher == nil {
		return v2input.GraphTask{}, v2input.ErrInvalid
	}
	entries, direct, err := MultiTaskInputEntries(ctx, plan, originals, qualified, params)
	if err != nil {
		return v2input.GraphTask{}, err
	}
	frozen := append([]v2input.Entry(nil), entries...)
	open := func(ctx context.Context) (v2input.Cursor, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return &perspectiveEntryCursor{entries: frozen}, nil
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
