package interchange

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/editor"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type ImportMaskCandidateArchive interface {
	Read(context.Context, v2input.Ref, string) ([]byte, error)
}

type ImportPersonMaskCandidate struct {
	ExternalID string                          `json:"external_id"`
	Decision   ImportPersonMaskDecisionRequest `json:"decision"`
}

// ExtractImportPersonMaskCandidates traverses the exact import entry and its
// archived recipe/overrides. It does not grant consent or infer identity from
// archived confirmation flags; each result still needs an explicit decision.
func ExtractImportPersonMaskCandidates(ctx context.Context, parsedRaw []byte, entryID, targetVersion uuid.UUID, archive ImportMaskCandidateArchive) ([]ImportPersonMaskCandidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if archive == nil || entryID == uuid.Nil || targetVersion == uuid.Nil || len(parsedRaw) < 2 || len(parsedRaw) > 8<<20 {
		return nil, v2input.ErrInvalid
	}
	normalized, err := canonical.Bytes(parsedRaw)
	if err != nil || !bytes.Equal(normalized, parsedRaw) {
		return nil, v2input.ErrInvalid
	}
	if _, err := validateParsedImportDocument(ctx, parsedRaw); err != nil {
		return nil, err
	}
	var parsed importParsedDocument
	if err := decodeImportDocument(parsedRaw, &parsed); err != nil {
		return nil, err
	}
	files := make(map[string]importParsedFile, len(parsed.Files))
	inventory := make(map[uuid.UUID]importBundleInventory, len(parsed.Manifest.Inventory))
	for _, file := range parsed.Files {
		files[file.Path] = file
	}
	for _, item := range parsed.Manifest.Inventory {
		inventory[item.ID] = item
	}
	var selected *importBundleEntry
	for i := range parsed.Manifest.Entries {
		if parsed.Manifest.Entries[i].ID == entryID {
			selected = &parsed.Manifest.Entries[i]
			break
		}
	}
	if selected == nil || selected.RecipePath == nil || selected.RecipeSHA == nil {
		return nil, v2input.ErrInvalid
	}
	recipeFile := files[*selected.RecipePath]
	if recipeFile.Contract != "EditingRecipeV3" || recipeFile.Content.SHA256 != *selected.RecipeSHA {
		return nil, v2input.ErrInvalid
	}
	recipeRaw, err := archive.Read(ctx, recipeFile.Content, "EditingRecipeV3")
	if err != nil {
		return nil, err
	}
	if len(recipeRaw) < 2 || len(recipeRaw) > 1<<20 || rawSHA(recipeRaw) != recipeFile.Content.SHA256 {
		return nil, v2input.ErrInvalid
	}
	var recipe struct {
		Source    v2input.SessionVersionBinding `json:"source"`
		Overrides []v2input.Ref                 `json:"overrides"`
	}
	if err := json.Unmarshal(recipeRaw, &recipe); err != nil {
		return nil, err
	}
	if err := v2input.ValidateEditingRecipeV3(recipeRaw, recipe.Source); err != nil {
		return nil, err
	}
	if recipe.Source.AssetVersionID == targetVersion {
		return nil, v2input.ErrInvalid
	}
	result := make([]ImportPersonMaskCandidate, 0)
	for _, archivedOverride := range recipe.Overrides {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		item, ok := inventory[archivedOverride.ArtifactID]
		if !ok || item.Contract != "Override" || item.SHA != archivedOverride.SHA256 {
			return nil, v2input.ErrInvalid
		}
		file, ok := files[item.Path]
		if !ok || file.Contract != "Override" || file.Content.SHA256 != item.SHA {
			return nil, v2input.ErrInvalid
		}
		overrideRaw, err := archive.Read(ctx, file.Content, "Override")
		if err != nil {
			return nil, err
		}
		if len(overrideRaw) < 2 || len(overrideRaw) > 8<<20 || rawSHA(overrideRaw) != item.SHA {
			return nil, v2input.ErrInvalid
		}
		var override struct {
			MaskRefs []struct {
				Mask      json.RawMessage `json:"mask"`
				Confirmed bool            `json:"confirmed"`
			} `json:"mask_refs"`
		}
		if err := json.Unmarshal(overrideRaw, &override); err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		for _, entry := range override.MaskRefs {
			if !entry.Confirmed {
				continue
			}
			mask, err := review.ParseMaskRef(entry.Mask)
			if err != nil || mask.Selector != "person" || mask.ObjectID != nil || mask.ModelLockID == nil {
				continue
			}
			if seen[string(entry.Mask)] {
				return nil, v2input.ErrInvalid
			}
			seen[string(entry.Mask)] = true
			pixel, ok := inventory[mask.Artifact.ID]
			if !ok || pixel.Contract != "MaskPixels" || pixel.SHA != mask.Artifact.SHA256 {
				return nil, v2input.ErrInvalid
			}
			if err := editor.ValidateOverride(overrideRaw, mask.Canvas.SourceSHA256); err != nil {
				return nil, err
			}
			result = append(result, ImportPersonMaskCandidate{ExternalID: selected.External, Decision: ImportPersonMaskDecisionRequest{
				Action: "grant", EntryID: entryID, TargetVersionID: targetVersion,
				OverrideArtifactID: archivedOverride.ArtifactID, MaskArtifactID: mask.Artifact.ID,
				ParsedJCS: string(parsedRaw), RecipeArtifactID: recipeFile.Content.ArtifactID,
				RecipeJCS: string(recipeRaw), OverrideJCS: string(overrideRaw), MaskJCS: string(entry.Mask),
			}})
		}
	}
	return result, ctx.Err()
}
