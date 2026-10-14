package v2input

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"io"
)

// BindPublishedTransport reopens only the frozen input cursor. Caller-supplied
// cursors/direct roots are never used; persisted document authority is SQL's job.
func (f FrozenSemanticTask) BindPublishedTransport(ctx context.Context, built Built) (PreparedSemanticTask, error) {
	if err := ctx.Err(); err != nil {
		return PreparedSemanticTask{}, err
	}
	if _, err := f.DraftTask(); err != nil {
		return PreparedSemanticTask{}, err
	}
	return bindFrozenTaskTransport(ctx, f.task, f.params, f.profile, f.key, f.closure, built)
}

func bindFrozenTaskTransport(ctx context.Context, task uuid.UUID, params Ref, profile json.RawMessage, key string, closure CaptureClosure, built Built) (PreparedSemanticTask, error) {
	if err := ctx.Err(); err != nil {
		return PreparedSemanticTask{}, err
	}
	if task == uuid.Nil || !validRef(params) || !json.Valid(profile) || closure.Open == nil {
		return PreparedSemanticTask{}, ErrInvalid
	}
	if !validBuilt(built, closure.Direct) {
		return PreparedSemanticTask{}, ErrInvalid
	}
	cursor, err := closure.Open(ctx)
	if err != nil {
		return PreparedSemanticTask{}, err
	}
	if cursor == nil {
		return PreparedSemanticTask{}, ErrInvalid
	}
	if closer, ok := cursor.(io.Closer); ok {
		defer closer.Close()
	}
	h := sha256.New()
	h.Write([]byte("["))
	count := 0
	for {
		if err := ctx.Err(); err != nil {
			return PreparedSemanticTask{}, err
		}
		entry, ok, err := cursor.Next(ctx)
		if err != nil {
			return PreparedSemanticTask{}, err
		}
		if !ok {
			break
		}
		if count >= built.Count {
			return PreparedSemanticTask{}, ErrInvalid
		}
		if err := writeEntryDigest(h, entry, count == 0); err != nil {
			return PreparedSemanticTask{}, err
		}
		count++
	}
	h.Write([]byte("]"))
	if count != built.Count || hex.EncodeToString(h.Sum(nil)) != built.SHA256 {
		return PreparedSemanticTask{}, ErrInvalid
	}
	owned := built
	owned.Chunks = append([]Ref(nil), built.Chunks...)
	return PreparedSemanticTask{Graph: GraphTask{ID: task, Built: owned, Open: closure.Open, Direct: append([]Ref(nil), closure.Direct...)}, Params: params, ParamsJCS: append([]byte(nil), closure.ParamsJCS...), Profile: append([]byte(nil), profile...), InputDigest: closure.InputDigest, NodeKey: key}, nil
}
