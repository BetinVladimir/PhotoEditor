package delivery

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/provenance"
	"photoeditor/backend/internal/v2input"
)

// DelegatedRenderInputCatalog reads the exact registered input inventory.
// It is a registration check, not a substitute for traversal of the typed
// dependency graph or physical verification of each occurrence.
type DelegatedRenderInputCatalog struct{ Tasks SQLDelegatedTaskCatalog }

func (s DelegatedRenderInputCatalog) ReadDelegatedRenderInputs(ctx context.Context,
	task provenance.CanonicalSigningTask, source v2input.Ref) ([]v2input.Entry, error) {
	if err := ctx.Err(); err != nil { return nil, err }
	if s.Tasks.DB == nil || task.Task == uuid.Nil || task.Operation != "render.compose" || task.Version != "1" {
		return nil, v2input.ErrInvalid
	}
	if source.ArtifactID == uuid.Nil || !shaPattern.MatchString(source.SHA256) ||
		task.Params.ArtifactID == uuid.Nil || !shaPattern.MatchString(task.Params.SHA256) ||
		source.ArtifactID == task.Params.ArtifactID { return nil, v2input.ErrInvalid }
	expectedTask, err := canonical.Marshal(task)
	if err != nil { return nil, err }
	checkTask := func() error {
		current, err := s.Tasks.ReadDelegatedTask(ctx, task.Task)
		if err != nil { return err }
		wire, err := canonical.Marshal(current)
		if err != nil || !bytes.Equal(wire, expectedTask) { return v2input.ErrInvalid }
		return nil
	}
	if err := checkTask(); err != nil { return nil, err }
	read := func() ([]v2input.Entry, []byte, error) {
		var raw []byte
		err := s.Tasks.DB.QueryRow(ctx, `SELECT photo.v31_delivery_render_input_inventory(
		 $1,$2,$3,$4,$5)`, s.Tasks.Lease.Expansion, s.Tasks.Lease.Token,
			s.Tasks.Slot.Ordinal, s.Tasks.Slot.PresetID, task.Task).Scan(&raw)
		if err != nil { return nil, nil, err }
		if len(raw) < 2 || len(raw) > 1<<20 { return nil, nil, v2input.ErrInvalid }
		var entries []v2input.Entry
		if json.Unmarshal(raw, &entries) != nil || len(entries) < 2 || len(entries) > 4096 {
			return nil, nil, v2input.ErrInvalid
		}
		if err := verifyDelegatedRenderInputInventory(task, source, entries); err != nil {
			return nil, nil, err
		}
		owned, err := canonical.Marshal(entries)
		if err != nil { return nil, nil, err }
		return entries, owned, nil
	}
	entries, first, err := read()
	if err != nil { return nil, err }
	if err := checkTask(); err != nil { return nil, err }
	_, second, err := read()
	if err != nil { return nil, err }
	if !bytes.Equal(first, second) { return nil, v2input.ErrInvalid }
	return entries, ctx.Err()
}

func verifyDelegatedRenderInputInventory(task provenance.CanonicalSigningTask,
	source v2input.Ref, entries []v2input.Entry) error {
	if len(entries) < 2 || len(entries) > 4096 || task.Task == uuid.Nil ||
		task.Operation != "render.compose" || task.Version != "1" ||
		task.Params.ArtifactID == uuid.Nil || !shaPattern.MatchString(task.Params.SHA256) ||
		source.ArtifactID == uuid.Nil || !shaPattern.MatchString(source.SHA256) ||
		source.ArtifactID == task.Params.ArtifactID { return v2input.ErrInvalid }
	params, working := 0, 0
	for i, entry := range entries {
		if entry.Ref.ArtifactID == uuid.Nil || !shaPattern.MatchString(entry.Ref.SHA256) ||
			len(entry.Contract) < 1 || len(entry.Contract) > 120 ||
			(i > 0 && bytes.Compare(entries[i-1].Ref.ArtifactID[:], entry.Ref.ArtifactID[:]) >= 0) {
			return v2input.ErrInvalid
		}
		if entry.Ref == task.Params && entry.Contract == "FrozenRenderRecipe" { params++ }
		if entry.Ref == source && entry.Contract == "WorkingImage" { working++ }
	}
	if params != 1 || working != 1 { return v2input.ErrInvalid }
	inputSHA, err := canonical.Hash(struct {
		Task uuid.UUID `json:"task"`
		Operation string `json:"operation"`
		Version string `json:"version"`
		Entries []v2input.Entry `json:"entries"`
	}{task.Task, task.Operation, task.Version, entries})
	if err != nil || inputSHA != task.InputDigest { return v2input.ErrInvalid }
	return nil
}
