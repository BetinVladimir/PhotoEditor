package v2render

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
	"strconv"
)

// CompositionInputAdmission binds the compiler's frozen inventory to the saved
// plan and repeats lens/user-mask authority while registering input rows. It
// seals only task inputs, not the runnable graph or operation qualification.
type CompositionInputAdmission struct{ DB *pgxpool.Pool }

func (a CompositionInputAdmission) RegisterLensUserInputs(ctx context.Context, tid, run, workflow uuid.UUID, ordinal int, draft v2input.DraftTask, graph v2input.GraphTask) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if a.DB == nil || tid == uuid.Nil || run == uuid.Nil || graph.Open == nil || graph.ID != draft.ID || graph.Built.Count < 1 || graph.Built.Count > 512 || len(graph.Direct) != 1 || graph.Direct[0] != (v2input.Ref{ArtifactID: draft.ParamsArtifactID, SHA256: draft.ParamsSHA}) {
		return v2input.ErrInvalid
	}
	expectedWorkflow, err := v2input.WorkflowID(run)
	if err != nil || expectedWorkflow != workflow {
		return v2input.ErrInvalid
	}
	expectedTask, err := v2input.TaskID(workflow, "layer.compose", ordinal)
	if err != nil || expectedTask != draft.ID {
		return v2input.ErrInvalid
	}
	cursor, err := graph.Open(ctx)
	if err != nil {
		return err
	}
	if cursor == nil {
		return v2input.ErrInvalid
	}
	closeCursor := func() error {
		if closer, ok := cursor.(io.Closer); ok {
			return closer.Close()
		}
		return nil
	}
	entries := make([]v2input.Entry, 0, graph.Built.Count)
	for {
		entry, ok, e := cursor.Next(ctx)
		if e != nil {
			_ = closeCursor()
			return e
		}
		if !ok {
			break
		}
		if len(entries) >= graph.Built.Count {
			_ = closeCursor()
			return v2input.ErrInvalid
		}
		entries = append(entries, entry)
	}
	if err := closeCursor(); err != nil {
		return err
	}
	digest, err := compositionInputDigest(draft.ID, entries)
	if err != nil || digest != draft.InputDigest || len(entries) != graph.Built.Count {
		return v2input.ErrInvalid
	}
	built := graph.Built
	built.Chunks = append([]v2input.Ref(nil), built.Chunks...)
	direct := append([]v2input.Ref(nil), graph.Direct...)
	return database.InTransaction(ctx, a.DB, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_scheduler" {
			return v2input.ErrInvalid
		}
		var accepted bool
		if err := tx.QueryRow(ctx, `SELECT photo.stage_v25_lens_user_masked_composition_task($1,$2,$3,$4,$5,$6,$7,$8::jsonb)`, tid, run, draft.ID, ordinal, draft.ParamsArtifactID, draft.ParamsSHA, draft.InputDigest, draft.ExecutionProfile).Scan(&accepted); err != nil {
			return err
		}
		if !accepted {
			return v2input.ErrExpired
		}
		var ready bool
		var storedManifest, chunksRaw json.RawMessage
		var storedSHA, storedCount *string
		if err := tx.QueryRow(ctx, `SELECT sealed,manifest,entries_sha,entry_count,chunks FROM photo.v25_composition_input_state($1,$2,$3)`, tid, run, draft.ID).Scan(&ready, &storedManifest, &storedSHA, &storedCount, &chunksRaw); err != nil {
			return err
		}
		if ready {
			var prior v2input.Ref
			var chunks []v2input.Ref
			if json.Unmarshal(storedManifest, &prior) != nil || prior != built.Manifest || storedSHA == nil || *storedSHA != built.SHA256 || storedCount == nil || *storedCount != strconv.Itoa(built.Count) || json.Unmarshal(chunksRaw, &chunks) != nil || len(chunks) != len(built.Chunks) {
				return v2input.ErrInvalid
			}
			for i, chunk := range chunks {
				if chunk != built.Chunks[i] {
					return v2input.ErrInvalid
				}
			}
			return nil
		}
		return v2input.RegisterInTransaction(ctx, tx, tid, run, draft.ID, built, func(ctx context.Context) (v2input.Cursor, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return &compositionCursor{entries: entries}, nil
		}, direct)
	})
}
