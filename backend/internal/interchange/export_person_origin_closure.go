package interchange

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// ValidateExportPersonOriginSourceClosure joins signed-receipt expectations to
// the frozen plan and independently qualified portable source inventory. It
// supplements, rather than replaces, the physical receipt and signature gate.
func ValidateExportPersonOriginSourceClosure(ctx context.Context, intent CreateIntent,
	frozen FrozenExportDocuments, raw []byte, inventory ExportInputInventory,
	expected map[string]PortablePersonOriginExpectation,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if expected == nil {
		return v2input.ErrInvalid
	}
	_, compiled, err := DecodeFrozenExportDocuments(ctx, intent, frozen.Body())
	if err != nil {
		return err
	}
	var plan importPlanDocument
	if err := decodeImportDocument(compiled.planBody, &plan); err != nil {
		return err
	}
	var manifest exportBundleManifest
	if err := decodeImportDocument(raw, &manifest); err != nil {
		return err
	}
	if plan.Direction != "export" || plan.Format != "photoeditor_bundle" ||
		inventory.task != compiled.task || len(manifest.PersonOrigins) != len(expected) ||
		len(expected) > 5000 {
		return v2input.ErrInvalid
	}
	entries := make(map[uuid.UUID]v2input.Entry, len(inventory.entries))
	for _, entry := range inventory.entries {
		if !validImportRef(entry.Ref) {
			return v2input.ErrInvalid
		}
		if _, duplicate := entries[entry.Ref.ArtifactID]; duplicate {
			return v2input.ErrInvalid
		}
		entries[entry.Ref.ArtifactID] = entry
	}
	planEntries := make(map[uuid.UUID]importPlanEntry, len(plan.Entries))
	for _, entry := range plan.Entries {
		if _, duplicate := planEntries[entry.ID]; duplicate {
			return v2input.ErrInvalid
		}
		planEntries[entry.ID] = entry
	}
	seen := map[string]bool{}
	for _, origin := range manifest.PersonOrigins {
		if err := ctx.Err(); err != nil {
			return err
		}
		scope, ok := expected[origin.Path]
		planEntry, inPlan := planEntries[origin.EntryID]
		if !ok || !inPlan || seen[origin.Path] || scope.EntryID != origin.EntryID ||
			planEntry.Binding == nil || *planEntry.Binding != scope.Source ||
			planEntry.Recipe == nil || *planEntry.Recipe != scope.Recipe ||
			scope.Recipe != origin.Recipe || scope.Override != origin.Override ||
			scope.MaskRef != origin.MaskRef || scope.MaskPixels != origin.MaskPixels ||
			!portablePersonOriginSHA.MatchString(scope.ModelLockSHA) {
			return v2input.ErrInvalid
		}
		for _, child := range []struct {
			ref      v2input.Ref
			contract string
		}{{scope.Recipe, "EditingRecipeV3"}, {scope.Override, "Override"},
			{scope.MaskRef, "MaskRef"}, {scope.MaskPixels, "MaskPixels"}} {
			actual, present := entries[child.ref.ArtifactID]
			if !present || actual.Ref != child.ref || actual.Contract != child.contract {
				return v2input.ErrInvalid
			}
		}
		seen[origin.Path] = true
	}
	return ctx.Err()
}
