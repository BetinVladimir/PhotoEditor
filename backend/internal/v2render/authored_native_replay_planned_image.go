package v2render

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

func (s SQLAuthoredNativeReplayCatalog) plannedImageGeometry(ctx context.Context, who auth.Identity, tenant uuid.UUID, node v2draft.LayerRef) (v2draft.DocumentGeometry, bool, error) {
	empty := v2draft.DocumentGeometry{}
	if node.Contract != "WorkingImage" {
		return empty, false, nil
	}
	// The exact original typed inventory must use its ordinary bootstrap
	// geometry. Asking the authored Geometry provider to reconstruct it before
	// the original source has initialized traversal creates a circular anchor.
	if s.originalEntries[v2input.Entry{Ref: node.Ref, Contract: node.Contract}] {
		return empty, false, nil
	}
	inventory, err := s.plannedInventory(ctx, who, tenant)
	if err != nil {
		return empty, false, err
	}
	bodies := map[v2input.Ref][]byte{}
	for _, entry := range inventory.Phases {
		var phase AuthoredPhaseReceipt
		if decodeAuthoredParams([]byte(entry.ReceiptJCS), &phase) != nil {
			return empty, false, v2input.ErrInvalid
		}
		if phase.Kind == "geometry" {
			raw, e := s.read(ctx, who, tenant, phase.Result)
			if e != nil {
				return empty, false, e
			}
			bodies[phase.Result] = raw
		}
	}
	// The original binding is fixed by the actual base receipt, never by the
	// requested node. Native and registered occurrences remain distinct identities.
	var base struct {
		Original  v2input.SessionVersionBinding `json:"original_source"`
		Processed v2input.SessionVersionBinding `json:"processed_source"`
	}
	if json.Unmarshal([]byte(inventory.BaseReceiptJCS), &base) != nil {
		return empty, false, v2input.ErrInvalid
	}
	allowed := node.Ref == base.Original.Source || node.Ref == base.Processed.Source
	expectedCanvas := ""
	expectedTransform := ""
	var expectedColor *review.ColorSpace
	bind := func(canvas, transform string) bool {
		if canvas != "" && expectedCanvas != "" && canvas != expectedCanvas {
			return false
		}
		if transform != "" && expectedTransform != "" && transform != expectedTransform {
			return false
		}
		if canvas != "" {
			expectedCanvas = canvas
		}
		if transform != "" {
			expectedTransform = transform
		}
		return true
	}
	if node.Ref == base.Original.Source && !bind(base.Original.CanvasHash, "") {
		return empty, false, v2input.ErrInvalid
	}
	if node.Ref == base.Processed.Source && !bind(base.Processed.CanvasHash, "") {
		return empty, false, v2input.ErrInvalid
	}
	for _, entry := range inventory.Phases {
		var phase AuthoredPhaseReceipt
		if decodeAuthoredParams([]byte(entry.ReceiptJCS), &phase) != nil {
			return empty, false, v2input.ErrInvalid
		}
		if phase.OutputSource.Source == node.Ref {
			allowed = true
			if !bind(phase.OutputSource.CanvasHash, "") {
				return empty, false, v2input.ErrInvalid
			}
		}
		if phase.Geometry != nil {
			if phase.Geometry.Image == node.Ref {
				geometry, err := s.nativeGeometry(ctx, who, tenant, phase)
				if err != nil {
					return empty, false, err
				}
				if !bind(geometry.CanvasHash, geometry.TransformHash) {
					return empty, false, v2input.ErrInvalid
				}
				allowed = true
			}
			geometry, color, found, err := authoredFinalRegisteredGeometry(phase, bodies[phase.Result], node.Ref)
			if err != nil {
				return empty, false, err
			}
			if found {
				if !bind(geometry.CanvasHash, geometry.TransformHash) {
					return empty, false, v2input.ErrInvalid
				}
				if expectedColor != nil && *expectedColor != color {
					return empty, false, v2input.ErrInvalid
				}
				expectedColor = &color
				allowed = true
			}
		}
	}
	if !allowed {
		// Original branches retain their existing per-occurrence geometry semantics.
		if s.originalEntries[v2input.Entry{Ref: node.Ref, Contract: node.Contract}] {
			return empty, false, nil
		}
		return empty, false, v2input.ErrInvalid
	}
	originalRaw, err := s.read(ctx, who, tenant, base.Original.Source)
	if err != nil {
		return empty, false, err
	}
	original, err := review.ParseWorkingImage(originalRaw)
	if err != nil {
		return empty, false, v2input.ErrInvalid
	}
	raw, err := s.read(ctx, who, tenant, node.Ref)
	if err != nil {
		return empty, false, err
	}
	image, err := review.ParseWorkingImage(raw)
	if err != nil || image.Canvas.SourceSHA256 != original.Canvas.SourceSHA256 || image.Color != original.Color || expectedColor != nil && image.Color != *expectedColor {
		return empty, false, v2input.ErrInvalid
	}
	canvas, err := canonical.Hash(image.Canvas)
	if err != nil || expectedCanvas != "" && canvas != expectedCanvas || node.CanvasHash != "" && canvas != node.CanvasHash {
		return empty, false, v2input.ErrInvalid
	}
	transform, err := canonical.Hash(image.TransformChain)
	if err != nil {
		return empty, false, err
	}
	if expectedTransform != "" && transform != expectedTransform {
		return empty, false, v2input.ErrInvalid
	}
	after, err := s.plannedInventory(ctx, who, tenant)
	if err != nil {
		return empty, false, err
	}
	beforeWire, err := canonical.Marshal(inventory)
	if err != nil {
		return empty, false, err
	}
	afterWire, err := canonical.Marshal(after)
	if err != nil {
		return empty, false, err
	}
	if !bytes.Equal(beforeWire, afterWire) {
		return empty, false, v2input.ErrInvalid
	}
	return v2draft.DocumentGeometry{CanvasHash: canvas, TransformHash: transform}, true, nil
}
