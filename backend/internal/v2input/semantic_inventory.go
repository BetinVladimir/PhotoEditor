package v2input

import (
	"context"
	"io"
)

// InputEntries returns a bounded copy of the compiler-owned frozen inventory.
// It performs no object IO and does not accept an external cursor.
func (f FrozenSemanticTask) InputEntries(ctx context.Context) ([]Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := f.DraftTask(); err != nil {
		return nil, err
	}
	cursor, err := f.closure.Open(ctx)
	if err != nil {
		return nil, err
	}
	if cursor == nil {
		return nil, ErrInvalid
	}
	if closer, ok := cursor.(io.Closer); ok {
		defer closer.Close()
	}
	var entries []Entry
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entry, ok, err := cursor.Next(ctx)
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		if len(entries) >= 128 {
			return nil, ErrInvalid
		}
		entries = append(entries, entry)
	}
	if len(entries) == 0 {
		return nil, ErrInvalid
	}
	return entries, nil
}
