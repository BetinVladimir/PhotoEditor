package interchange

import (
	"context"
	"encoding/json"
	"sync"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type preparedLUTWitness struct {
	Parent   v2input.Ref
	Geometry ExportArrayGeometry
}
type ImportPreparedLUTWitnesses struct {
	mu    sync.RWMutex
	nodes map[v2input.Ref][]preparedLUTWitness
}

func (w *ImportPreparedLUTWitnesses) Remember(ctx context.Context, parent v2input.Entry, raw []byte, binding v2input.SessionVersionBinding, children []v2input.Entry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if w == nil || parent.Contract != "ResolvedStyle" || !validImportRef(parent.Ref) || rawSHA(raw) != parent.Ref.SHA256 {
		return v2input.ErrInvalid
	}
	var body struct {
		Params struct {
			Image v2input.Ref `json:"image"`
		} `json:"params"`
		LUT *struct {
			Artifact v2input.Ref `json:"artifact"`
			Size     int         `json:"size"`
			DType    string      `json:"dtype"`
			Order    string      `json:"order"`
			Profile  string      `json:"profile_id"`
		} `json:"absolute_lut"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return err
	}
	if body.Params.Image != binding.Source {
		return v2input.ErrInvalid
	}
	if body.LUT == nil {
		return nil
	}
	lut := body.LUT
	if !validImportRef(lut.Artifact) || (lut.Size != 33 && lut.Size != 65) || lut.DType != "float32" || lut.Order != "RGB_C" || lut.Profile != "work_rec2020_gamma24_v1" {
		return v2input.ErrInvalid
	}
	expected := v2input.Entry{Ref: lut.Artifact, Contract: "AbsoluteLUTArray"}
	found := 0
	for _, child := range children {
		if child.Ref.ArtifactID == lut.Artifact.ArtifactID {
			if child != expected {
				return v2input.ErrInvalid
			}
			found++
		}
	}
	if found != 1 {
		return v2input.ErrInvalid
	}
	digest, err := canonical.Hash(lut)
	if err != nil {
		return err
	}
	geometry := ExportArrayGeometry{Width: lut.Size, Height: lut.Size, CanvasSHA: digest, DType: "float32"}
	if _, err := exportArrayMaximumBytes(expected, geometry); err != nil {
		return err
	}
	item := preparedLUTWitness{Parent: parent.Ref, Geometry: geometry}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.nodes == nil {
		w.nodes = map[v2input.Ref][]preparedLUTWitness{}
	}
	for _, existing := range w.nodes[lut.Artifact] {
		if existing == item {
			return nil
		}
		if existing.Geometry != geometry {
			return v2input.ErrInvalid
		}
	}
	if len(w.nodes) >= 30000 || len(w.nodes[lut.Artifact]) >= 5000 {
		return v2input.ErrInvalid
	}
	w.nodes[lut.Artifact] = append(w.nodes[lut.Artifact], item)
	return ctx.Err()
}

func (w *ImportPreparedLUTWitnesses) lookup(ref v2input.Ref) []preparedLUTWitness {
	if w == nil {
		return nil
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	return append([]preparedLUTWitness(nil), w.nodes[ref]...)
}
