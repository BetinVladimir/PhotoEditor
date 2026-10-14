package v2input

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

// CompileDeliveryChildClosure pins caller-authorized transitive entries and
// independently compiled roots. Resolution/physical source authority is required
// before this function; it cannot establish completeness from a list of refs.
func CompileDeliveryChildClosure(workflow uuid.UUID, operation string, paramsRaw []byte, paramsRef Ref, roots, resolved []Entry) (CaptureClosure, error) {
 return compileDeliveryChildClosure(workflow,operation,paramsRaw,paramsRef,roots,resolved,false)
}

func CompileDeliveryWatermarkClosure(workflow uuid.UUID,paramsRaw []byte,paramsRef Ref,roots,resolved []Entry)(CaptureClosure,error){
 return compileDeliveryChildClosure(workflow,"derivative.watermark",paramsRaw,paramsRef,roots,resolved,true)
}

func compileDeliveryChildClosure(workflow uuid.UUID, operation string, paramsRaw []byte, paramsRef Ref, roots, resolved []Entry,watermark bool) (CaptureClosure, error) {
	if !(watermark && operation=="derivative.watermark") && operation != "derivative.resize" && operation != "derivative.sharpen" && operation != "export.encode" {
		return CaptureClosure{}, ErrInvalid
	}
	task, err := TaskID(workflow, operation, 0)
	if err != nil || len(paramsRaw) < 2 || len(paramsRaw) > 65536 || !validRef(paramsRef) || len(roots) < 4 || len(resolved) > 1000000 {
		return CaptureClosure{}, ErrInvalid
	}
	exact, err := canonical.Marshal(json.RawMessage(paramsRaw))
	if err != nil || !bytes.Equal(exact, paramsRaw) {
		return CaptureClosure{}, ErrInvalid
	}
	inventory := make(map[uuid.UUID]Entry, len(resolved))
	contracts := make(map[string]bool)
	for _, entry := range resolved {
		if !validEntry(entry) {
			return CaptureClosure{}, ErrInvalid
		}
		if previous, ok := inventory[entry.Ref.ArtifactID]; ok && previous != entry {
			return CaptureClosure{}, ErrInvalid
		}
		inventory[entry.Ref.ArtifactID] = entry
	}
	var params struct {
		Image Ref `json:"image"`
 Watermark Ref `json:"watermark"`
	}
	if json.Unmarshal(paramsRaw, &params) != nil || !validRef(params.Image) || inventory[params.Image.ArtifactID] != (Entry{params.Image, "WorkingImage"}) {
		return CaptureClosure{}, ErrInvalid
	}
	for _, root := range roots {
		if !validEntry(root) || inventory[root.Ref.ArtifactID] != root {
			return CaptureClosure{}, ErrInvalid
		}
		contracts[root.Contract] = true
	}
	if watermark {
  if !validRef(params.Watermark)||params.Watermark.ArtifactID==params.Image.ArtifactID||inventory[params.Watermark.ArtifactID]!=(Entry{params.Watermark,"WorkingImage"}){return CaptureClosure{},ErrInvalid}
  imageRoot,markRoot:=false,false
  for _,root:=range roots{imageRoot=imageRoot||root==(Entry{params.Image,"WorkingImage"});markRoot=markRoot||root==(Entry{params.Watermark,"WorkingImage"})}
  if !imageRoot||!markRoot{return CaptureClosure{},ErrInvalid}
 }
 for _, contract := range []string{"WorkingImage", "NpyArray", "EditingRecipeV3", "ExportPresetConfig"} {
		if !contracts[contract] {
			return CaptureClosure{}, ErrInvalid
		}
	}
	entries := append([]Entry(nil), resolved...)
	entries = append(entries, Entry{paramsRef, "Params"})
	closure, err := compileCaptureInventory(task, operation, json.RawMessage(paramsRaw), paramsRef, entries)
	if err != nil {
		return CaptureClosure{}, err
	}
	// Direct inputs are the independently compiled stage roots and Params.
	// Transitive inventory remains complete in the owned cursor/chunks.
	direct := make([]Ref, 0, len(roots)+1)
	seen := make(map[uuid.UUID]bool)
	for _, root := range append(append([]Entry(nil), roots...), Entry{paramsRef, "Params"}) {
		if !seen[root.Ref.ArtifactID] {
			direct = append(direct, root.Ref)
			seen[root.Ref.ArtifactID] = true
		}
	}
	if len(direct) > 127 {
		return CaptureClosure{}, ErrInvalid
	}
	closure.Direct = direct
	return closure, nil
}
