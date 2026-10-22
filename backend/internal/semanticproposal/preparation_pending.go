package semanticproposal

import (
	"errors"
	"photoeditor/backend/internal/v2input"
)

// ErrPreparationPending distinguishes a durable private prerequisite from an
// incompatible public request. It never carries a parent admission ticket.
var ErrPreparationPending = errors.New("semantic preparation pending")

func ordinaryScenePreparationPending() error {
	// Internal recovery callers historically recognize the private conflict.
	// HTTP must check the explicit pending marker before generic conflicts.
	return errors.Join(ErrPreparationPending, v2input.ErrConflict)
}
