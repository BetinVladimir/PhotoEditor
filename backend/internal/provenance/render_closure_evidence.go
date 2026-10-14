package provenance

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type SigningRenderClosureResolver interface {
	ResolveTaskInputEvidence(context.Context, auth.Identity, uuid.UUID, []v2input.Entry, v2input.SessionVersionBinding) (v2draft.TaskInputEvidence, error)
}
type BoundSigningRenderClosureSource struct {
	Documents SigningTaskDocumentReader
	Closure   SigningRenderClosureResolver
}

// This is scoped dependency evidence, not producer ancestry or permission to
// execute a finished task. Installation requires a historical adopted catalog.
type SigningRenderClosureEvidence struct {
	taskDigest string
	inventory  v2draft.TaskInputEvidence
}

func (s BoundSigningRenderClosureSource) ReadSigningRenderClosure(ctx context.Context, who auth.Identity, ready ReadySigningExport, task CanonicalSigningTask) (SigningRenderClosureEvidence, error) {
	var zero SigningRenderClosureEvidence
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.Closure == nil {
		return zero, ErrInvalid
	}
	recipe, err := ReadSigningRenderRecipe(ctx, s.Documents, who, ready, task)
	if err != nil {
		return zero, err
	}
	raw, err := canonical.Marshal(recipe)
	if err != nil {
		return zero, err
	}
	roots := []v2input.Entry{{Ref: recipe.Scalars.Source.Source, Contract: "WorkingImage"}, {Ref: task.Params, Contract: "FrozenRenderRecipe"}}
	read := func() (v2draft.TaskInputEvidence, []byte, error) {
		evidence, err := s.Closure.ResolveTaskInputEvidence(ctx, who, ready.Tenant, append([]v2input.Entry(nil), roots...), recipe.Scalars.Source)
		if err != nil {
			return v2draft.TaskInputEvidence{}, nil, err
		}
		if err := verifySigningRenderInputDigest(task, evidence.Entries); err != nil {
			return v2draft.TaskInputEvidence{}, nil, err
		}
		// Require the exact frozen root bytes actually witnessed by traversal.
		found := false
		for _, document := range evidence.Documents {
			if document.Ref == task.Params && document.Contract == "FrozenRenderRecipe" {
				if found || document.JCS != string(raw) {
					return v2draft.TaskInputEvidence{}, nil, ErrInvalid
				}
				found = true
			}
		}
		if !found || evidence.Entries == nil || evidence.Occurrences == nil {
			return v2draft.TaskInputEvidence{}, nil, ErrInvalid
		}
		frozen, err := canonical.Marshal(evidence)
		if err != nil || len(frozen) > 64<<20 {
			return v2draft.TaskInputEvidence{}, nil, ErrInvalid
		}
		var owned v2draft.TaskInputEvidence
		if json.Unmarshal(frozen, &owned) != nil {
			return v2draft.TaskInputEvidence{}, nil, ErrInvalid
		}
		return owned, frozen, nil
	}
	inventory, proof, err := read()
	if err != nil {
		return zero, err
	}
	if err := VerifySigningRenderTransport(ctx, s.Documents, who, ready, task, inventory.Entries); err != nil {
		return zero, err
	}
	_, current, err := read()
	if err != nil {
		return zero, err
	}
	if !bytes.Equal(proof, current) {
		return zero, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	digest, err := canonical.Hash(task)
	if err != nil {
		return zero, err
	}
	return SigningRenderClosureEvidence{taskDigest: digest, inventory: inventory}, nil
}

var _ SigningRenderClosureResolver = v2draft.ClosureResolver{}

// Identical to CompileRenderDraft's content digest, not the transport refs list.
func verifySigningRenderInputDigest(task CanonicalSigningTask, entries []v2input.Entry) error {
	if task.Task == uuid.Nil || task.Operation != "render.compose" || task.Version != "1" || len(entries) < 2 || len(entries) > 4096 {
		return ErrInvalid
	}
	for i, entry := range entries {
		if !ref(entry.Ref) || (len(entry.Contract) < 1 || len(entry.Contract) > 120) || (i > 0 && bytes.Compare(entries[i-1].Ref.ArtifactID[:], entry.Ref.ArtifactID[:]) >= 0) {
			return ErrInvalid
		}
	}
	digest, err := canonical.Hash(struct {
		Task      uuid.UUID       `json:"task"`
		Operation string          `json:"operation"`
		Version   string          `json:"version"`
		Entries   []v2input.Entry `json:"entries"`
	}{task.Task, task.Operation, task.Version, entries})
	if err != nil || digest != task.InputDigest {
		return ErrInvalid
	}
	return nil
}
