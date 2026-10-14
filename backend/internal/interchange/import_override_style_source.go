package interchange

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// ImportOverrideStyleSource derives the base StyleV2 only from the exact
// parsed bundle entry and its immutable archived EditingRecipeV3. It never
// guesses the style from a portable asset ID or a caller-provided style ref.
type ImportOverrideStyleSource struct {
	Parsed []byte
	Entry  uuid.UUID
	Old    v2input.SessionVersionBinding
	Reader ImportArchiveDocumentReader
}

func (s ImportOverrideStyleSource) ReadBaseStyle(ctx context.Context, override v2input.Ref) (v2input.Ref, error) {
	zero := v2input.Ref{}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.Reader == nil || s.Entry == uuid.Nil || !validImportRef(override) || s.Old.AssetVersionID == uuid.Nil || !validImportRef(s.Old.Source) {
		return zero, v2input.ErrInvalid
	}
	if _, err := validateParsedImportDocument(ctx, s.Parsed); err != nil {
		return zero, err
	}
	var parsed importParsedDocument
	if err := decodeImportDocument(s.Parsed, &parsed); err != nil {
		return zero, err
	}
	var recipePath, recipeSHA string
	for _, entry := range parsed.Manifest.Entries {
		if entry.ID != s.Entry {
			continue
		}
		if recipePath != "" || entry.RecipePath == nil || entry.RecipeSHA == nil {
			return zero, v2input.ErrInvalid
		}
		recipePath, recipeSHA = *entry.RecipePath, *entry.RecipeSHA
	}
	if recipePath == "" {
		return zero, v2input.ErrInvalid
	}
	var staged v2input.Ref
	var recipeFound, styleFound, overrideFound bool
	for _, item := range parsed.Manifest.Inventory {
		if item.Path == recipePath {
			if recipeFound || item.Contract != "EditingRecipeV3" || item.SHA != recipeSHA {
				return zero, v2input.ErrInvalid
			}
			recipeFound = true
		}
		if item.ID == override.ArtifactID {
			if overrideFound || item.Contract != "Override" || item.SHA != override.SHA256 {
				return zero, v2input.ErrInvalid
			}
			overrideFound = true
		}
	}
	for _, file := range parsed.Files {
		if file.Path == recipePath {
			if staged != zero || file.Contract != "EditingRecipeV3" || file.Content.SHA256 != recipeSHA {
				return zero, v2input.ErrInvalid
			}
			staged = file.Content
		}
	}
	if !recipeFound || !overrideFound || !validImportRef(staged) {
		return zero, v2input.ErrInvalid
	}
	read := func() ([]byte, error) {
		raw, err := s.Reader.ReadImportArchiveDocument(ctx, staged, 1<<20)
		if err != nil {
			return nil, err
		}
		raw = append([]byte(nil), raw...)
		if len(raw) < 2 || len(raw) > 1<<20 || rawSHA(raw) != recipeSHA || v2input.ValidateEditingRecipeV3(raw, s.Old) != nil {
			return nil, v2input.ErrInvalid
		}
		return raw, ctx.Err()
	}
	raw, err := read()
	if err != nil {
		return zero, err
	}
	var recipe struct {
		BaseStyle v2input.Ref   `json:"base_style"`
		Overrides []v2input.Ref `json:"overrides"`
	}
	if err := json.Unmarshal(raw, &recipe); err != nil {
		return zero, err
	}
	matched := false
	for _, ref := range recipe.Overrides {
		if ref == override {
			matched = true
		}
	}
	if !matched || !validImportRef(recipe.BaseStyle) {
		return zero, v2input.ErrInvalid
	}
	for _, item := range parsed.Manifest.Inventory {
		if item.ID == recipe.BaseStyle.ArtifactID && item.SHA == recipe.BaseStyle.SHA256 && item.Contract == "StyleV2" {
			styleFound = true
		}
	}
	if !styleFound {
		return zero, v2input.ErrInvalid
	}
	current, err := read()
	if err != nil {
		return zero, err
	}
	if !bytes.Equal(current, raw) {
		return zero, v2input.ErrInvalid
	}
	return recipe.BaseStyle, ctx.Err()
}
