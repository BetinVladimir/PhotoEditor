package interchange

import (
	"context"
	"encoding/json"
	"sync"

	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type preparedDefectLabelWitness struct {
	Parent v2input.Ref
	Width  int64
	Height int64
}

type ImportPreparedDefectLabelWitnesses struct {
	mu    sync.RWMutex
	nodes map[v2input.Ref][]preparedDefectLabelWitness
}

// Called only after ImportCodecDocumentValidator has qualified the complete
// prepared proposal and projected its exact children.
func (w *ImportPreparedDefectLabelWitnesses) Remember(ctx context.Context, parent v2input.Entry, raw []byte, binding v2input.SessionVersionBinding, children []v2input.Entry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if w == nil || parent.Contract != "DefectCandidates" || !validImportRef(parent.Ref) || rawSHA(raw) != parent.Ref.SHA256 {
		return v2input.ErrInvalid
	}
	verified, err := review.ParseDefectProposalStructure(ctx, raw, review.ArtifactRef{ID: parent.Ref.ArtifactID, SHA256: parent.Ref.SHA256})
	if err != nil {
		return err
	}
	bindingInfo, err := verified.LabelBinding()
	if err != nil || bindingInfo.Proposal.ID != parent.Ref.ArtifactID || bindingInfo.Proposal.SHA256 != parent.Ref.SHA256 {
		return v2input.ErrInvalid
	}
	var projection struct {
		Image v2input.Ref `json:"image"`
	}
	if err := json.Unmarshal(raw, &projection); err != nil || projection.Image != binding.Source {
		return v2input.ErrInvalid
	}
	label := v2input.Entry{Ref: v2input.Ref{ArtifactID: bindingInfo.Labels.ID, SHA256: bindingInfo.Labels.SHA256}, Contract: "DefectLabelMap"}
	if !validImportRef(label.Ref) || bindingInfo.Width < 1 || bindingInfo.Height < 1 || bindingInfo.Width*bindingInfo.Height > 200000000 {
		return v2input.ErrInvalid
	}
	count := 0
	for _, child := range children {
		if child.Ref.ArtifactID == label.Ref.ArtifactID {
			if child != label {
				return v2input.ErrInvalid
			}
			count++
		}
	}
	if count != 1 {
		return v2input.ErrInvalid
	}
	item := preparedDefectLabelWitness{Parent: parent.Ref, Width: bindingInfo.Width, Height: bindingInfo.Height}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.nodes == nil {
		w.nodes = map[v2input.Ref][]preparedDefectLabelWitness{}
	}
	for _, existing := range w.nodes[label.Ref] {
		if existing == item {
			return nil
		}
		if existing.Width != item.Width || existing.Height != item.Height {
			return v2input.ErrInvalid
		}
	}
	if len(w.nodes) >= 30000 || len(w.nodes[label.Ref]) >= 5000 {
		return v2input.ErrInvalid
	}
	w.nodes[label.Ref] = append(w.nodes[label.Ref], item)
	return ctx.Err()
}

func (w *ImportPreparedDefectLabelWitnesses) lookup(ref v2input.Ref) []preparedDefectLabelWitness {
	if w == nil {
		return nil
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	return append([]preparedDefectLabelWitness(nil), w.nodes[ref]...)
}
