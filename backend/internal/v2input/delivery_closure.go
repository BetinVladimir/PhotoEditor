package v2input

import "github.com/google/uuid"

// DeliveryPrepareParams references an immutable, hash-verified delivery input.
type DeliveryPrepareParams struct {
	SchemaVersion int `json:"schema_version"`
	Input         Ref `json:"input"`
}

// CompileDeliveryPrepareClosure owns and pins a resolved inventory. The caller
// must resolve every transitive snapshot/recipe/image/config dependency under
// current project authority before calling. This function does not establish
// that authority or prove that a supplied inventory is transitively complete.
// requiredRoots are independently obtained from the frozen input and snapshot;
// each must occur with its exact contract in resolved. No missing root is added.
func CompileDeliveryPrepareClosure(workflow uuid.UUID, params DeliveryPrepareParams, paramsRef Ref, requiredRoots, resolved []Entry) (CaptureClosure, error) {
	if params.SchemaVersion != 1 || !validRef(params.Input) || !validRef(paramsRef) || len(requiredRoots) < 2 || len(resolved) > 1000000 {
		return CaptureClosure{}, ErrInvalid
	}
	task, err := TaskID(workflow, "v2.delivery.prepare", 0)
	if err != nil {
		return CaptureClosure{}, err
	}
	inventory := make(map[uuid.UUID]Entry, len(resolved))
	for _, entry := range resolved {
		if !validEntry(entry) {
			return CaptureClosure{}, ErrInvalid
		}
		if previous, exists := inventory[entry.Ref.ArtifactID]; exists && previous != entry {
			return CaptureClosure{}, ErrInvalid
		}
		inventory[entry.Ref.ArtifactID] = entry
	}
	frozen := Entry{params.Input, "FrozenDeliveryInput"}
	if inventory[params.Input.ArtifactID] != frozen {
		return CaptureClosure{}, ErrInvalid
	}
	hasSnapshot := false
	for _, root := range requiredRoots {
		if !validEntry(root) || inventory[root.Ref.ArtifactID] != root {
			return CaptureClosure{}, ErrInvalid
		}
		if root.Contract == "InputSnapshot" {
			hasSnapshot = true
		}
	}
	if !hasSnapshot {
		return CaptureClosure{}, ErrInvalid
	}
	// Copy before the common compiler sorts; callers retain their original order.
	entries := make([]Entry, 0, len(resolved)+1)
	entries = append(entries, resolved...)
	entries = append(entries, Entry{paramsRef, "Params"})
	closure, err := compileCaptureInventory(task, "v2.delivery.prepare", params, paramsRef, entries)
	if err != nil {
		return CaptureClosure{}, err
	}
	// Nested dependencies remain in the verified indexed inventory, not the
	// bounded task.inputs transport envelope (maximum 127 direct references).
	closure.Direct = []Ref{paramsRef, params.Input}
	return closure, nil
}
