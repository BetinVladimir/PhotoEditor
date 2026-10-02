package v2render

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
	"strconv"
)

// RenderInputAdmission binds frozen inventory to the saved render plan and
// repeats staged recipe authority while registering input rows. It
// seals only task inputs, not the runnable graph or operation qualification.
type RenderInputAdmission struct{ DB *pgxpool.Pool }

func (a RenderInputAdmission) RegisterRenderInputs(ctx context.Context, tid, run, workflow uuid.UUID, ordinal int, draft v2input.DraftTask, graph v2input.GraphTask) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if a.DB == nil || tid == uuid.Nil || run == uuid.Nil || graph.Open == nil || graph.ID != draft.ID || graph.Built.Count < 1 || graph.Built.Count > 4096 || len(graph.Direct) != 1 || graph.Direct[0] != (v2input.Ref{ArtifactID: draft.ParamsArtifactID, SHA256: draft.ParamsSHA}) {
		return v2input.ErrInvalid
	}
	expectedWorkflow, err := v2input.WorkflowID(run)
	if err != nil || expectedWorkflow != workflow {
		return v2input.ErrInvalid
	}
	expectedTask, err := v2input.TaskID(workflow, "render.compose", ordinal)
	if err != nil || expectedTask != draft.ID || draft.Operation != "render.compose" || draft.OperationVersion != "1" || draft.NodeKey != "render.compose:"+strconv.Itoa(ordinal) || draft.ResourceClass != "gpu" {
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
	rootSeen, sourceSeen := false, false
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
		if !validLayerArtifactRef(entry.Ref) || len(entry.Contract) < 1 || len(entry.Contract) > 120 || entry.Contract == "InputClosureManifest" || entry.Contract == "InputClosureChunk" || (len(entries) > 0 && bytes.Compare(entries[len(entries)-1].Ref.ArtifactID[:], entry.Ref.ArtifactID[:]) >= 0) {
			_ = closeCursor()
			return v2input.ErrInvalid
		}
		if entry.Ref.ArtifactID == draft.ParamsArtifactID {
			if entry.Ref.SHA256 != draft.ParamsSHA || entry.Contract != "FrozenRenderRecipe" {
				_ = closeCursor()
				return v2input.ErrInvalid
			}
			rootSeen = true
		}
		if entry.Contract == "WorkingImage" {
			sourceSeen = true
		}
		entries = append(entries, entry)
	}
	if err := closeCursor(); err != nil {
		return err
	}
	digest, err := canonical.Hash(struct {
		Task      uuid.UUID       `json:"task"`
		Operation string          `json:"operation"`
		Version   string          `json:"version"`
		Entries   []v2input.Entry `json:"entries"`
	}{draft.ID, "render.compose", "1", entries})
	if err != nil || digest != draft.InputDigest || len(entries) != graph.Built.Count || !rootSeen || !sourceSeen {
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
		if err := tx.QueryRow(ctx, `SELECT photo.stage_v26_render_recipe_task($1,$2,$3,$4,$5,$6,$7,$8::jsonb)`, tid, run, draft.ID, ordinal, draft.ParamsArtifactID, draft.ParamsSHA, draft.InputDigest, draft.ExecutionProfile).Scan(&accepted); err != nil {
			return err
		}
		if !accepted {
			return v2input.ErrExpired
		}
		var ready bool
		var storedManifest, chunksRaw json.RawMessage
		var storedSHA, storedCount *string
		if err := tx.QueryRow(ctx, `SELECT sealed,manifest,entries_sha,entry_count,chunks FROM photo.v26_render_input_state($1,$2,$3)`, tid, run, draft.ID).Scan(&ready, &storedManifest, &storedSHA, &storedCount, &chunksRaw); err != nil {
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
