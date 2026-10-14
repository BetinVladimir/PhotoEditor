package v2input

import "github.com/google/uuid"

// DeliveryAssembleParams freezes the exact published and retained assembly root.
type DeliveryAssembleParams struct {
	SchemaVersion int `json:"schema_version"`
	Assembly      Ref `json:"assembly"`
}

// CompileDeliveryAssemblyClosure pins an independently resolved full inventory.
// Required roots must be derived from verified root/shard bytes by the caller;
// this compiler proves identity and completeness relative to those roots, not
// current SQL authority or physical object readiness.
func CompileDeliveryAssemblyClosure(workflow uuid.UUID, params DeliveryAssembleParams, paramsRef Ref, requiredRoots, resolved []Entry) (CaptureClosure, error) {
	if params.SchemaVersion != 1 || !validRef(params.Assembly) || !validRef(paramsRef) || len(requiredRoots) < 3 || len(requiredRoots) > 1000000 || len(resolved) > 1000000 {
		return CaptureClosure{}, ErrInvalid
	}
	task, err := TaskID(workflow, "v2.delivery.assemble", 0)
	if err != nil {
		return CaptureClosure{}, err
	}
	inventory := make(map[uuid.UUID]Entry, len(resolved))
	for _, entry := range resolved {
		if !validEntry(entry) {
			return CaptureClosure{}, ErrInvalid
		}
		if previous, ok := inventory[entry.Ref.ArtifactID]; ok && previous != entry {
			return CaptureClosure{}, ErrInvalid
		}
		inventory[entry.Ref.ArtifactID] = entry
	}
	root := Entry{Ref: params.Assembly, Contract: "DeliveryAssemblyInput"}
	if inventory[params.Assembly.ArtifactID] != root {
		return CaptureClosure{}, ErrInvalid
	}
	rootSeen, shards, configs := false, 0, 0
	seen := map[uuid.UUID]bool{}
	for _, entry := range requiredRoots {
		if !validEntry(entry) || inventory[entry.Ref.ArtifactID] != entry || seen[entry.Ref.ArtifactID] {
			return CaptureClosure{}, ErrInvalid
		}
		seen[entry.Ref.ArtifactID] = true
		if entry == root {
			rootSeen = true
		}
		switch entry.Contract {
		case "DeliverySlotShard":
			shards++
		case "ExportPresetConfig":
			configs++
		}
	}
	if !rootSeen || shards < 1 || shards > 160 || configs < 1 || configs > 16 {
		return CaptureClosure{}, ErrInvalid
	}
	entries := append([]Entry(nil), resolved...)
	entries = append(entries, Entry{Ref: paramsRef, Contract: "Params"})
	closure, err := compileCaptureInventory(task, "v2.delivery.assemble", params, paramsRef, entries)
	if err != nil {
		return CaptureClosure{}, err
	}
	// Nested dependencies remain in the verified indexed inventory, not the
	// bounded task.inputs transport envelope (maximum 127 direct references).
	closure.Direct = []Ref{paramsRef, params.Assembly}
	return closure, nil
}
