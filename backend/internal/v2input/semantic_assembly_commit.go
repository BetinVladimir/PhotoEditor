package v2input

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

// CommitSemanticAssembly validates the reopened immutable inventory before any
// SQL mutation, then registers and closes the expansion in one server statement.
func (a Admission) CommitSemanticAssembly(ctx context.Context, tenant, run, expansion, token uuid.UUID, task GraphTask) error {
	if a.DB == nil || tenant == uuid.Nil || expansion == uuid.Nil || token == uuid.Nil || task.Open == nil || task.Built.Count > 50000 || !validBuilt(task.Built, task.Direct) || len(task.Direct) != 4 {
		return ErrInvalid
	}
	workflow, err := WorkflowID(run)
	if err != nil {
		return err
	}
	expected, err := TaskID(workflow, "v2.semantic.assemble", 0)
	if err != nil || expected != task.ID {
		return ErrInvalid
	}
	entries, err := semanticCommitInventory(ctx, task)
	if err != nil {
		return err
	}
	manifest, err := canonical.Marshal(task.Built.Manifest)
	if err != nil {
		return err
	}
	chunks, err := canonical.Marshal(task.Built.Chunks)
	if err != nil {
		return err
	}
	direct, err := canonical.Marshal(task.Direct)
	if err != nil {
		return err
	}
	var ok bool
	err = a.DB.QueryRow(ctx, `SELECT photo.commit_v27_semantic_assembly($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, tenant, run, expansion, token, task.ID, json.RawMessage(entries), json.RawMessage(manifest), json.RawMessage(chunks), task.Built.SHA256, json.RawMessage(direct)).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return ErrInvalid
	}
	return nil
}

func semanticCommitInventory(ctx context.Context, task GraphTask) ([]byte, error) {
	if task.Open == nil || task.Built.Count < 4 || task.Built.Count > 50000 {
		return nil, ErrInvalid
	}
	cursor, err := task.Open(ctx)
	if err != nil {
		return nil, err
	}
	if cursor == nil {
		return nil, ErrInvalid
	}
	if closer, ok := cursor.(io.Closer); ok {
		defer closer.Close()
	}
	entries := make([]Entry, 0, task.Built.Count)
	h := sha256.New()
	_, _ = h.Write([]byte("["))
	var prior uuid.UUID
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
		if !validEntry(entry) || len(entries) >= task.Built.Count || (len(entries) > 0 && bytes.Compare(prior[:], entry.Ref.ArtifactID[:]) >= 0) {
			return nil, ErrInvalid
		}
		if err := writeEntryDigest(h, entry, len(entries) == 0); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
		prior = entry.Ref.ArtifactID
	}
	_, _ = h.Write([]byte("]"))
	if len(entries) != task.Built.Count || hex.EncodeToString(h.Sum(nil)) != task.Built.SHA256 {
		return nil, ErrInvalid
	}
	body, err := canonical.Marshal(entries)
	if err != nil || len(body) > 16<<20 {
		return nil, ErrInvalid
	}
	return body, nil
}
