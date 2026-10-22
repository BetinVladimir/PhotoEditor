package v2render

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// SQLAuthoredDetailMaterialization owns the physical-read → original-mask path
// compilation → protected publication → actual segment continuation boundary.
// Task identities only select protected readback; no caller supplies transform
// output bytes, producer decisions, or replacement portrait regions.
type SQLAuthoredDetailMaterialization struct {
	Continuation SQLAuthoredDetailContinuation
	Original     SQLAuthoredPhaseObjects
	Transforms   SQLAuthoredDetailTransformReader
	Masks        AuthoredLayerMaskPublication
}

func (s SQLAuthoredDetailMaterialization) Resume(ctx context.Context, who auth.Identity, tenant, parent uuid.UUID, ordinal int,
	styleBody []byte, styleWitnesses map[string]string, transformTasks []uuid.UUID, layerTemplate []byte, maskBindings json.RawMessage, protected *v2input.Ref) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	db := s.Continuation.Style.Scheduler
	if db == nil || s.Transforms.Scheduler != db || s.Original.API == nil || s.Original.Bucket != s.Transforms.Bucket || s.Original.Bucket != s.Continuation.Style.Bucket || len(transformTasks) > 32896 {
		return false, v2input.ErrInvalid
	}
	source, current, err := ReadAuthoredInstructionSource(ctx, db, tenant, parent, ordinal)
	if err != nil {
		return false, err
	}
	if !current {
		return false, v2input.ErrExpired
	}
	if source.Actor != who.UserID || source.Session != who.SessionID || source.Instruction.Kind != "segment" || len(source.Instruction.Layers) != 1 {
		return false, v2input.ErrInvalid
	}
	var stack struct {
		Layers []struct {
			ID        uuid.UUID   `json:"id"`
			Operation string      `json:"operation"`
			Version   string      `json:"operation_version"`
			Params    v2input.Ref `json:"params"`
		} `json:"layers"`
	}
	if json.Unmarshal([]byte(source.LayerStackJCS), &stack) != nil {
		return false, v2input.ErrInvalid
	}
	selected := v2input.Ref{}
	count := 0
	for _, layer := range stack.Layers {
		if layer.ID == source.Instruction.Layers[0] {
			if layer.Operation != "portrait.detail" || layer.Version != "1" {
				return false, v2input.ErrInvalid
			}
			selected = layer.Params
			count++
		}
	}
	if count != 1 || !validLayerArtifactRef(selected) {
		return false, v2input.ErrInvalid
	}
	original, err := s.Original.ReadJSON(ctx, who, tenant, source.Project, parent, ordinal, selected)
	if err != nil {
		return false, err
	}
	image, err := s.Original.ReadJSON(ctx, who, tenant, source.Project, parent, ordinal, source.Source.Source)
	if err != nil {
		return false, err
	}
	// Supplied style compiler witnesses must include these same physically read
	// originals. The concrete style publisher checks all remaining witnesses.
	if styleWitnesses["authored_params_jcs"] != string(original) || styleWitnesses["current_image_jcs"] != string(image) {
		return false, v2input.ErrInvalid
	}
	documents := make([]AuthoredDetailTransformDocument, 0, len(transformTasks))
	proofs := make([]json.RawMessage, 0, len(transformTasks))
	seen := map[uuid.UUID]bool{}
	for _, task := range transformTasks {
		if task == uuid.Nil || seen[task] {
			return false, v2input.ErrInvalid
		}
		seen[task] = true
		proof, found, err := s.Transforms.Read(ctx, who, source, task)
		if err != nil {
			return false, err
		}
		if !found {
			return false, ErrAuthoredSegmentPending
		}
		documents = append(documents, proof.Document())
		proofs = append(proofs, proof.Proof())
	}
	transforms, err := canonical.Marshal(proofs)
	if err != nil {
		return false, err
	}
	return s.Continuation.ResumeMaterialized(ctx, who, tenant, parent, ordinal, styleBody, styleWitnesses, original, image, source.Source.Source, documents, transforms, layerTemplate, maskBindings, protected)
}
