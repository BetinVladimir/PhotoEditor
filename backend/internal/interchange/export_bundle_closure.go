package interchange

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"sort"
)

// ResolveExportBundleSourceInventory walks only encoded images and portable
// recipe roots, including all their typed descendants. Snapshot/transport-only
// roots are intentionally excluded from the portable archive. Resolver must
// independently qualify every node with current source/native/runtime authority.
func ResolveExportBundleSourceInventory(ctx context.Context, intent CreateIntent, frozen FrozenExportDocuments, resolver ExportClosureNodeResolver) (ExportInputInventory, error) {
	_, compiled, err := DecodeFrozenExportDocuments(ctx, intent, frozen.Body())
	if err != nil {
		return ExportInputInventory{}, err
	}
	var plan importPlanDocument
	if err := decodeImportDocument(compiled.planBody, &plan); err != nil {
		return ExportInputInventory{}, err
	}
	if plan.Format != "photoeditor_bundle" {
		return ExportInputInventory{}, v2input.ErrInvalid
	}
	// Preserve full frozen source/geometry authority before limiting archive
	// roots. Otherwise graph-bound native adapters lose FrozenRenderRecipe parents.
	if factory, ok := resolver.(ExportGraphResolverFactory); ok {
		bound, err := factory.BindExportGraph(ctx, compiled)
		if err != nil {
			return ExportInputInventory{}, err
		}
		resolver = bound
	}
	backgroundRoots := []v2input.Entry{}
	backgrounds := map[string]bundleBackground{}
	for _, root := range compiled.roots {
		if root.Contract == "FrozenRenderRecipe" {
			node, err := resolver.ResolveExportNode(ctx, root)
			if err != nil {
				return ExportInputInventory{}, err
			}
			if node.Entry != root || rawSHA(node.Body) != root.Ref.SHA256 {
				return ExportInputInventory{}, v2input.ErrInvalid
			}
			recipe, err := v2draft.DecodeFrozenRenderRecipe(node.Body)
			if err != nil {
				return ExportInputInventory{}, err
			}
			matched := false
			for _, entry := range plan.Entries {
				if entry.Binding != nil && *entry.Binding == recipe.Scalars.Source && entry.Recipe != nil && entry.Recipe.SHA256 == recipe.RecipeSHA256 {
					matched = true
				}
			}
			if !matched {
				return ExportInputInventory{}, v2input.ErrInvalid
			}
			if pin := recipe.BackgroundAsset; pin != nil {
				child := v2input.Entry{Ref: pin.Source, Contract: "StyleBackgroundAsset"}
				found := false
				for _, edge := range node.Children {
					if edge == child {
						found = true
					}
				}
				if !found {
					return ExportInputInventory{}, v2input.ErrInvalid
				}
				mapping := bundleBackground{Style: pin.Style, Portable: pin.PortableAssetID, Path: "dependencies/" + pin.Source.ArtifactID.String() + ".bin", SHA: pin.Source.SHA256}
				key := pin.Style.ArtifactID.String() + ":" + pin.PortableAssetID
				if old, exists := backgrounds[key]; exists && old != mapping {
					return ExportInputInventory{}, v2input.ErrInvalid
				}
				backgrounds[key] = mapping
				backgroundRoots = append(backgroundRoots, child)
			}
		}
	}
	compiled.roots = nil
	for _, entry := range plan.Entries {
		if entry.Recipe == nil {
			return ExportInputInventory{}, v2input.ErrInvalid
		}
		compiled.roots = append(compiled.roots, v2input.Entry{Ref: entry.Content, Contract: "EncodedImage"}, v2input.Entry{Ref: *entry.Recipe, Contract: "EditingRecipeV3"})
	}
	compiled.roots = append(compiled.roots, backgroundRoots...)
	inventory, err := ResolveExportInputInventory(ctx, compiled, resolver)
	if err != nil {
		return ExportInputInventory{}, err
	}
	for _, mapping := range backgrounds {
		inventory.backgrounds = append(inventory.backgrounds, mapping)
	}
	sort.Slice(inventory.backgrounds, func(i, j int) bool {
		a, b := inventory.backgrounds[i], inventory.backgrounds[j]
		if a.Style.ArtifactID == b.Style.ArtifactID {
			return a.Portable < b.Portable
		}
		return a.Style.ArtifactID.String() < b.Style.ArtifactID.String()
	})
	if len(inventory.backgrounds) > 0 {
		inventory.backgroundDigest, err = canonical.Hash(inventory.backgrounds)
		if err != nil {
			return ExportInputInventory{}, err
		}
	}
	return inventory, ctx.Err()
}

// ValidateExportBundleSourceInventory binds a manifest to a qualified recipe
// closure receipt. This must follow physical/manifest validation. A receipt
// needs current authority rechecks and immutable persistence before SQL adoption.
func ValidateExportBundleSourceInventory(ctx context.Context, intent CreateIntent, frozen FrozenExportDocuments, raw []byte, inventory ExportInputInventory) error {
	return validateExportBundleSourceInventory(ctx, intent, frozen, raw, inventory, nil)
}

// ValidateExportBundleSourceInventoryWithPersonOrigins admits the two portable
// files per signed occurrence only after the caller supplies source-derived
// expectations. Physical ZIP, signature, and issuer checks remain mandatory.
func ValidateExportBundleSourceInventoryWithPersonOrigins(ctx context.Context, intent CreateIntent, frozen FrozenExportDocuments, raw []byte, inventory ExportInputInventory, origins map[string]PortablePersonOriginExpectation) error {
	if origins == nil {
		return v2input.ErrInvalid
	}
	if err := ValidateExportPersonOriginSourceClosure(ctx, intent, frozen, raw, inventory, origins); err != nil {
		return err
	}
	return validateExportBundleSourceInventory(ctx, intent, frozen, raw, inventory, origins)
}
func validateExportBundleSourceInventory(ctx context.Context, intent CreateIntent, frozen FrozenExportDocuments, raw []byte, inventory ExportInputInventory, origins map[string]PortablePersonOriginExpectation) error {
	if err := ctx.Err(); err != nil {
		return err
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
	if plan.Format != "photoeditor_bundle" || (origins == nil && len(manifest.PersonOrigins) > 0) || inventory.task != compiled.task || len(inventory.entries) == 0 || len(inventory.entries) > 30000 || len(manifest.Inventory) > 30000 {
		return v2input.ErrInvalid
	}
	digest, err := canonical.Hash(inventory.entries)
	if err != nil || digest != inventory.digest {
		return v2input.ErrInvalid
	}
	if len(manifest.Backgrounds) != len(inventory.backgrounds) {
		return v2input.ErrInvalid
	}
	if len(inventory.backgrounds) > 0 {
		expectedDigest, err := canonical.Hash(inventory.backgrounds)
		if err != nil || expectedDigest != inventory.backgroundDigest {
			return v2input.ErrInvalid
		}
		actualDigest, err := canonical.Hash(manifest.Backgrounds)
		if err != nil || actualDigest != expectedDigest {
			return v2input.ErrInvalid
		}
	}
	paths := map[uuid.UUID]string{}
	expected := map[uuid.UUID]v2input.Entry{}
	for _, entry := range plan.Entries {
		paths[entry.Content.ArtifactID] = entry.Path
	}
	binary := map[string]bool{"EncodedImage": true, "StyleBackgroundAsset": true, "WorkingPixels": true, "WorkingAlpha": true, "PixelArray": true, "MaskPixels": true, "MaskArray": true, "DenseWarpField": true, "DenseWarpArray": true}
	for _, entry := range inventory.entries {
		if !validImportRef(entry.Ref) {
			return v2input.ErrInvalid
		}
		if _, ok := expected[entry.Ref.ArtifactID]; ok {
			return v2input.ErrInvalid
		}
		expected[entry.Ref.ArtifactID] = entry
		if paths[entry.Ref.ArtifactID] == "" {
			suffix := ".json"
			if binary[entry.Contract] {
				suffix = ".bin"
			}
			prefix := "dependencies/"
			if entry.Contract == "EditingRecipeV3" {
				prefix = "recipes/"
			}
			paths[entry.Ref.ArtifactID] = prefix + entry.Ref.ArtifactID.String() + suffix
		}
	}
	if origins != nil {
		for path, scope := range origins {
			if !validImportRef(scope.MaskRef) || scope.EntryID == uuid.Nil || path != "origins/"+scope.EntryID.String()+"/"+scope.MaskRef.ArtifactID.String()+".json" {
				return v2input.ErrInvalid
			}
			maskPath := "dependencies/" + scope.MaskRef.ArtifactID.String() + ".json"
			mask := v2input.Entry{Ref: scope.MaskRef, Contract: "MaskRef"}
			if old, ok := expected[scope.MaskRef.ArtifactID]; ok {
				if old != mask || paths[scope.MaskRef.ArtifactID] != maskPath {
					return v2input.ErrInvalid
				}
			} else {
				expected[scope.MaskRef.ArtifactID] = mask
				paths[scope.MaskRef.ArtifactID] = maskPath
			}
			receiptID := uuid.NewSHA1(scope.EntryID, []byte("v30:person-origin:"+scope.MaskRef.ArtifactID.String()))
			receiptSHA := ""
			for _, origin := range manifest.PersonOrigins {
				if origin.Path == path {
					receiptSHA = origin.SHA
					break
				}
			}
			if !validImportRef(v2input.Ref{ArtifactID: receiptID, SHA256: receiptSHA}) || expected[receiptID].Contract != "" {
				return v2input.ErrInvalid
			}
			expected[receiptID] = v2input.Entry{Ref: v2input.Ref{ArtifactID: receiptID, SHA256: receiptSHA}, Contract: "PersonOriginReceipt"}
			paths[receiptID] = path
		}
	}
	if len(manifest.Inventory) != len(expected) {
		return v2input.ErrInvalid
	}
	for _, item := range manifest.Inventory {
		if err := ctx.Err(); err != nil {
			return err
		}
		entry, ok := expected[item.ID]
		if !ok || entry.Contract != item.Contract || entry.Ref.SHA256 != item.SHA || paths[item.ID] != item.Path {
			return v2input.ErrInvalid
		}
		delete(expected, item.ID)
	}
	if len(expected) != 0 {
		return v2input.ErrInvalid
	}
	return ctx.Err()
}
