package v2render

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// SQLAuthoredSegmentCatalog is specific to one current segment instruction. It does
// not infer geometry authority from a caller's canvas or from tenant membership.
// The SQL-selected, physically verified canonical transcript selects occurrences;
// the ordinary ClosureResolver retains its original-source and typed-root checks.
type SQLAuthoredSegmentCatalog struct {
	Scheduler       *pgxpool.Pool
	Objects         SQLAuthoredPhaseObjects
	Project, Parent uuid.UUID
	Ordinal         int
	original        v2draft.SQLArtifactCatalog
	paramsRef       v2input.Ref
	originalEntries map[v2input.Entry]bool
}

func (s SQLAuthoredSegmentCatalog) Lookup(ctx context.Context, who auth.Identity, tenant uuid.UUID, node v2draft.LayerRef) (objectstore.Object, error) {
	inventory, found, err := ReadAuthoredSegmentInventory(ctx, s.Scheduler, tenant, s.Parent, s.Ordinal)
	if err != nil {
		return objectstore.Object{}, err
	}
	if !found || inventory.Context.Project != s.Project || inventory.Context.Actor != who.UserID || inventory.Context.Session != who.SessionID {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	if s.originalEntries[v2input.Entry{Ref: node.Ref, Contract: node.Contract}] {
		return s.original.Lookup(ctx, who, tenant, node)
	}
	return s.Objects.Object(ctx, who, tenant, s.Project, s.Parent, s.Ordinal, node.Ref)
}

func (s SQLAuthoredSegmentCatalog) Geometry(ctx context.Context, who auth.Identity, tenant uuid.UUID, node v2draft.LayerRef) (v2draft.DocumentGeometry, bool, error) {
	empty := v2draft.DocumentGeometry{}
	if node.Contract == "MaskRef" {
		return s.maskGeometry(ctx, who, tenant, node)
	}
	if node.Contract != "WorkingImage" {
		return empty, false, nil
	}
	documents, found, err := ReadAuthoredSegmentDocuments(ctx, s.Scheduler, s.Objects, who, tenant, s.Parent, s.Ordinal)
	if err != nil {
		return empty, false, err
	}
	if !found {
		return empty, false, v2input.ErrInvalid
	}
	if documents.Inventory.Context.Project != s.Project {
		return empty, false, v2input.ErrInvalid
	}
	// ReadAuthoredSegmentDocuments rechecks the actual adopted prefix; no final-only continuation is required for a segment.
	// The original binding is fixed by the actual base receipt, never by the
	// requested node. Native and registered occurrences remain distinct identities.
	var base struct {
		Original  v2input.SessionVersionBinding `json:"original_source"`
		Processed v2input.SessionVersionBinding `json:"processed_source"`
	}
	if json.Unmarshal([]byte(documents.Inventory.BaseReceiptJCS), &base) != nil {
		return empty, false, v2input.ErrInvalid
	}
	// The exact original anchor must bootstrap ClosureResolver's source SHA
	// through its unchanged ordinary binding predicate, not a derived geometry hook.
	if node.Ref == base.Original.Source {
		return empty, false, nil
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
	for _, entry := range documents.Inventory.Phases {
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
			geometry, color, found, err := authoredFinalRegisteredGeometry(phase, documents.Bodies[phase.Result], node.Ref)
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
	originalRaw, err := s.Objects.ReadJSON(ctx, who, tenant, s.Project, s.Parent, s.Ordinal, base.Original.Source)
	if err != nil {
		return empty, false, err
	}
	original, err := review.ParseWorkingImage(originalRaw)
	if err != nil {
		return empty, false, v2input.ErrInvalid
	}
	raw, err := s.Objects.ReadJSON(ctx, who, tenant, s.Project, s.Parent, s.Ordinal, node.Ref)
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
	after, present, err := ReadAuthoredSegmentInventory(ctx, s.Scheduler, tenant, s.Parent, s.Ordinal)
	if err != nil {
		return empty, false, err
	}
	if !present {
		return empty, false, v2input.ErrInvalid
	}
	beforeWire, err := canonical.Marshal(documents.Inventory)
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

// NewSQLAuthoredSegmentCatalog resolves the original full recipe through its
// existing typed version authority. Delegation is limited to exact resolved
// contract/ref pairs; no failed authored lookup authorizes a broad fallback.
func NewSQLAuthoredSegmentCatalog(ctx context.Context, scheduler *pgxpool.Pool, objects SQLAuthoredPhaseObjects, who auth.Identity, tenant, parent uuid.UUID, ordinal int) (SQLAuthoredSegmentCatalog, error) {
	empty := SQLAuthoredSegmentCatalog{}
	inventory, found, err := ReadAuthoredSegmentInventory(ctx, scheduler, tenant, parent, ordinal)
	if err != nil {
		return empty, err
	}
	if !found || inventory.Context.Actor != who.UserID || inventory.Context.Session != who.SessionID {
		return empty, v2input.ErrInvalid
	}
	var base struct {
		Original v2input.SessionVersionBinding `json:"original_source"`
	}
	if json.Unmarshal([]byte(inventory.BaseReceiptJCS), &base) != nil || !authoredBindingValid(base.Original) {
		return empty, v2input.ErrInvalid
	}
	project := inventory.Context.Project
	recipeRaw, err := objects.ReadJSON(ctx, who, tenant, project, parent, ordinal, inventory.Context.OriginalRecipe)
	if err != nil {
		return empty, err
	}
	sourceRaw, err := objects.ReadJSON(ctx, who, tenant, project, parent, ordinal, base.Original.Source)
	if err != nil {
		return empty, err
	}
	original := v2draft.SQLArtifactCatalog{DB: objects.API, Bucket: objects.Bucket, ProjectID: project, AssetVersionID: base.Original.AssetVersionID}
	resolver := v2draft.ClosureResolver{Catalog: original, Objects: objects.Objects}
	entries, err := ResolveRenderInputs(ctx, who, tenant, base.Original, inventory.Context.OriginalRecipe, recipeRaw, sourceRaw, resolver)
	if err != nil {
		return empty, err
	}
	out := SQLAuthoredSegmentCatalog{Scheduler: scheduler, Objects: objects, Project: project, Parent: parent, Ordinal: ordinal, original: original, originalEntries: make(map[v2input.Entry]bool, len(entries))}
	for _, entry := range entries {
		out.originalEntries[entry] = true
	}
	after, present, err := ReadAuthoredSegmentInventory(ctx, scheduler, tenant, parent, ordinal)
	if err != nil {
		return empty, err
	}
	if !present {
		return empty, v2input.ErrInvalid
	}
	a, err := canonical.Marshal(inventory)
	if err != nil {
		return empty, err
	}
	b, err := canonical.Marshal(after)
	if err != nil || !bytes.Equal(a, b) {
		return empty, v2input.ErrInvalid
	}
	return out, nil
}

// The final catalog receiver here is only the existing operation-specific JSON
// geometry reader: its nativeGeometry methods do not select a final inventory or
// authorize fallback. All IO still uses the current segment parent/ordinal scope.
func (s SQLAuthoredSegmentCatalog) nativeGeometry(ctx context.Context, who auth.Identity, tenant uuid.UUID, phase AuthoredPhaseReceipt) (v2draft.DocumentGeometry, error) {
	reader := SQLAuthoredFinalCatalog{Objects: s.Objects, Project: s.Project, Parent: s.Parent, Ordinal: s.Ordinal}
	return reader.nativeGeometry(ctx, who, tenant, phase)
}
