package v2render

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// SQLAuthoredDetailMaskPreparation derives task identities from the published
// prefix and original Params. Native.Prepare obtains actual sizes under locks.
// The three budgets are trusted deployment limits, not invented input sizes.
type SQLAuthoredDetailMaskPreparation struct {
	Materialization      SQLAuthoredDetailMaterialization
	Native               AuthoredMaskTransformPreparation
	RAM, Scratch, Output int64
}

// Resume prepares at most one missing independent child per poll. Every adopted
// child is read physically again, before any next descriptor can depend on it.
func (p SQLAuthoredDetailMaskPreparation) Resume(ctx context.Context, who auth.Identity, tenant, parent uuid.UUID, ordinal int, protected *v2input.Ref) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	db := p.Materialization.Continuation.Style.Scheduler
	if db == nil || p.Native.Scheduler != db || p.Materialization.Transforms.Scheduler != db {
		return false, v2input.ErrInvalid
	}
	inventory, found, err := ReadAuthoredSegmentInventory(ctx, db, tenant, parent, ordinal)
	if err != nil {
		return false, err
	}
	if !found {
		return false, ErrAuthoredSegmentPending
	}
	source := inventory.Context
	if source.Actor != who.UserID || source.Session != who.SessionID {
		return false, v2input.ErrInvalid
	}
	paramsRef, expression, err := authoredDetailSelectedOccurrence(source)
	if err != nil {
		return false, err
	}
	read := func(ref v2input.Ref) ([]byte, error) {
		return p.Materialization.Original.ReadJSON(ctx, who, tenant, source.Project, parent, ordinal, ref)
	}
	active, err := authoredDetailOccurrenceActive(source)
	if err != nil {
		return false, err
	}
	var original []byte
	if active {
		original, err = read(paramsRef)
		if err != nil {
			return false, err
		}
	}
	var params struct {
		Regions []struct {
			Mask json.RawMessage `json:"mask"`
		} `json:"regions"`
		Protected []json.RawMessage `json:"protected"`
	}
	if active && json.Unmarshal(original, &params) != nil {
		return false, v2input.ErrInvalid
	}
	type occurrence struct {
		Mask                   review.MaskRef
		Params                 v2input.Ref
		ParamsJCS              string
		LayerRef, ProtectedRef *v2input.Ref
		LayerJCS, ProtectedJCS *string
	}
	masks := make([]occurrence, 0, len(params.Regions)+len(params.Protected))
	for _, r := range params.Regions {
		m, e := review.ParseMaskRef(r.Mask)
		if e != nil {
			return false, e
		}
		masks = append(masks, occurrence{Mask: m, Params: paramsRef, ParamsJCS: string(original)})
	}
	for _, raw := range params.Protected {
		m, e := review.ParseMaskRef(raw)
		if e != nil {
			return false, e
		}
		masks = append(masks, occurrence{Mask: m, Params: paramsRef, ParamsJCS: string(original)})
	}
	var refs []v2input.Ref
	if active {
		refs, err = authoredDetailExpressionRefs(ctx, expression)
	}
	if err != nil {
		return false, err
	}
	for _, ref := range refs {
		raw, e := read(ref)
		if e != nil {
			return false, e
		}
		m, e := review.ParseMaskRef(raw)
		if e != nil {
			return false, e
		}
		ownedRef := ref
		ownedJCS := string(raw)
		masks = append(masks, occurrence{Mask: m, Params: paramsRef, ParamsJCS: string(original), LayerRef: &ownedRef, LayerJCS: &ownedJCS})
	}
	// Global protection is discovered from the immutable recipe, never supplied
	// as a replacement ref. Its dedicated source authority is required by native
	// preparation before any task can be admitted.
	globalRef, err := authoredRecipeProtectionRef(source)
	if err != nil {
		return false, err
	}
	if globalRef != nil {
		raw, e := read(*globalRef)
		if e != nil {
			return false, e
		}
		mask, e := review.ParseMaskRef(raw)
		if e != nil {
			return false, e
		}
		ownedRef := *globalRef
		ownedJCS := string(raw)
		masks = append(masks, occurrence{Mask: mask, Params: source.OriginalRecipe, ParamsJCS: source.RecipeJCS, ProtectedRef: &ownedRef, ProtectedJCS: &ownedJCS})
	}
	tasks := []uuid.UUID{}
	seen := map[uuid.UUID]bool{}
	allProofs := []CurrentAuthoredDetailTransform{}
	transportedOriginals := map[string]bool{}
	for _, originalOccurrence := range masks {
		originalMask := originalOccurrence.Mask
		originalKey, e := canonical.Marshal(originalMask)
		if e != nil {
			return false, e
		}
		// Exact identical canonical original masks may occur in both inline and
		// standalone/global positions. One deterministic actual producer path can
		// serve those identical occurrences; no ID/settings/metadata mapping occurs.
		if transportedOriginals[string(originalKey)] {
			continue
		}
		transportedOriginals[string(originalKey)] = true
		current := originalMask
		var previous *AuthoredMaskTransformPrevious
		var previousJCS *string
		for _, item := range inventory.Phases {
			if err := ctx.Err(); err != nil {
				return false, err
			}
			if item.Kind != "geometry" {
				continue
			}
			var phase AuthoredPhaseReceipt
			if decodeAuthoredParams([]byte(item.ReceiptJCS), &phase) != nil {
				return false, v2input.ErrInvalid
			}
			targetRaw, e := read(phase.OutputSource.Source)
			if e != nil {
				return false, e
			}
			target, e := review.ParseWorkingImage(targetRaw)
			if e != nil {
				return false, e
			}
			if current.Canvas == target.Canvas && bytes.Equal(current.TransformChain, target.TransformChain) {
				continue
			}
			applicable, e := authoredMaskNextGeometry(current, target)
			if e != nil {
				return false, e
			}
			if !applicable {
				continue
			}
			d := AuthoredMaskTransformDescriptor{SchemaVersion: 1, Parent: parent, Instruction: source.Instruction.Identity, ProgramSHA: source.ProgramSHA, PredecessorSHA: source.PredecessorSHA, OriginalMask: originalMask, OriginalParams: originalOccurrence.Params, OriginalParamsJCS: originalOccurrence.ParamsJCS, OriginalProtectedRef: originalOccurrence.ProtectedRef, OriginalProtectedJCS: originalOccurrence.ProtectedJCS, OriginalLayerMaskRef: originalOccurrence.LayerRef, OriginalLayerMaskJCS: originalOccurrence.LayerJCS, SourceMask: current, Target: phase.OutputSource.Source, GeometryReceipt: item.Receipt, GeometryOrdinal: phase.Ordinal, GeometryTask: phase.TaskID, GeometryResult: phase.ResultID, GeometryAttempt: phase.Attempt, GeometryFence: phase.Fence, Previous: previous}
			_, _, task, e := AuthoredMaskTransformIDs(d)
			if e != nil {
				return false, e
			}
			proof, adopted, e := p.Materialization.Transforms.Read(ctx, who, source, task)
			if e != nil {
				return false, e
			}
			if !adopted {
				plan, e := p.Native.Prepare(ctx, source, d, targetRaw, previousJCS, p.RAM, p.Scratch, p.Output)
				if e != nil {
					return false, e
				}
				if e = p.Native.Admit(ctx, source, plan, previousJCS, string(targetRaw)); e != nil {
					return false, e
				}
				return false, ErrAuthoredSegmentPending
			}
			document := proof.Document()
			next, e := review.ParseTransformedMask(document.Root)
			if e != nil {
				return false, e
			}
			var tuple struct {
				Task    uuid.UUID   `json:"task_id"`
				Result  uuid.UUID   `json:"result_id"`
				Attempt int         `json:"attempt"`
				Fence   int64       `json:"fence"`
				Root    v2input.Ref `json:"root"`
			}
			if json.Unmarshal(proof.Proof(), &tuple) != nil || tuple.Task != task || tuple.Result == uuid.Nil || tuple.Attempt < 1 || tuple.Fence < 1 || !validLayerArtifactRef(tuple.Root) {
				return false, v2input.ErrInvalid
			}
			previous = &AuthoredMaskTransformPrevious{Task: task, Result: tuple.Result, Attempt: tuple.Attempt, Fence: tuple.Fence, Root: tuple.Root}
			raw := string(document.Root)
			previousJCS = &raw
			current = next
			if !seen[task] {
				seen[task] = true
				tasks = append(tasks, task)
				allProofs = append(allProofs, proof)
			}
		}
	}
	if !active {
		currentRaw, e := read(source.Source.Source)
		if e != nil {
			return false, e
		}
		derived, _, e := p.Materialization.publishRecipeProtection(ctx, who, source, currentRaw, allProofs)
		if e != nil {
			return false, e
		}
		if protected != nil && (derived == nil || *derived != *protected) {
			return false, v2input.ErrInvalid
		}
		return p.Materialization.Continuation.Segment.Resume(ctx, who, tenant, parent, ordinal, nil, derived)
	}
	return p.Materialization.ResumeOriginal(ctx, who, tenant, parent, ordinal, tasks, protected)
}

func authoredDetailSelectedOccurrence(source AuthoredInstructionSource) (v2input.Ref, json.RawMessage, error) {
	var stack struct {
		Layers []struct {
			ID        uuid.UUID       `json:"id"`
			Operation string          `json:"operation"`
			Version   string          `json:"operation_version"`
			Params    v2input.Ref     `json:"params"`
			Mask      json.RawMessage `json:"mask"`
		} `json:"layers"`
	}
	if len(source.Instruction.Layers) != 1 || json.Unmarshal([]byte(source.LayerStackJCS), &stack) != nil {
		return v2input.Ref{}, nil, v2input.ErrInvalid
	}
	var ref v2input.Ref
	var expression json.RawMessage
	count := 0
	for _, l := range stack.Layers {
		if l.ID == source.Instruction.Layers[0] {
			if l.Operation != "portrait.detail" || l.Version != "1" {
				return ref, nil, v2input.ErrInvalid
			}
			ref = l.Params
			expression = append(json.RawMessage(nil), l.Mask...)
			count++
		}
	}
	if count != 1 || !validLayerArtifactRef(ref) {
		return v2input.Ref{}, nil, v2input.ErrInvalid
	}
	return ref, expression, nil
}

// Earlier geometry is skipped only for an exact existing chain prefix. A
// divergent chain or a multi-step jump cannot borrow another producer's proof.
func authoredMaskNextGeometry(mask review.MaskRef, target review.WorkingImage) (bool, error) {
	var a, b struct {
		Steps []json.RawMessage `json:"steps"`
	}
	if json.Unmarshal(mask.TransformChain, &a) != nil || json.Unmarshal(target.TransformChain, &b) != nil || len(a.Steps) > 128 || len(b.Steps) > 128 {
		return false, v2input.ErrInvalid
	}
	n := len(a.Steps)
	if len(b.Steps) < n {
		n = len(b.Steps)
	}
	for i := 0; i < n; i++ {
		x, e := canonical.Bytes(a.Steps[i])
		if e != nil {
			return false, e
		}
		y, e := canonical.Bytes(b.Steps[i])
		if e != nil || !bytes.Equal(x, y) {
			return false, v2input.ErrInvalid
		}
	}
	if len(b.Steps) == len(a.Steps) {
		if mask.Canvas != target.Canvas {
			return false, v2input.ErrInvalid
		}
		return false, nil
	}
	if len(b.Steps) < len(a.Steps) {
		return false, nil
	}
	if len(b.Steps) != len(a.Steps)+1 {
		return false, v2input.ErrInvalid
	}
	var step struct {
		From review.Canvas `json:"from_canvas"`
		To   review.Canvas `json:"to_canvas"`
	}
	if json.Unmarshal(b.Steps[len(a.Steps)], &step) != nil || step.From != mask.Canvas || step.To != target.Canvas {
		return false, v2input.ErrInvalid
	}
	return true, nil
}

func authoredDetailOccurrenceActive(source AuthoredInstructionSource) (bool, error) {
	var stack struct {
		Layers []struct {
			ID      uuid.UUID `json:"id"`
			Enabled *bool     `json:"enabled"`
			Opacity *float64  `json:"opacity"`
		} `json:"layers"`
	}
	if len(source.Instruction.Layers) != 1 || json.Unmarshal([]byte(source.LayerStackJCS), &stack) != nil {
		return false, v2input.ErrInvalid
	}
	count := 0
	active := false
	for _, l := range stack.Layers {
		if l.ID == source.Instruction.Layers[0] {
			count++
			if l.Enabled == nil || l.Opacity == nil || *l.Opacity < 0 || *l.Opacity > 1 {
				return false, v2input.ErrInvalid
			}
			active = *l.Enabled && *l.Opacity > 0
		}
	}
	if count != 1 {
		return false, v2input.ErrInvalid
	}
	return active, nil
}
