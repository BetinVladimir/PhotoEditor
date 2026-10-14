package interchange

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/editor"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// This port validates complete module schemas and every portable asset/model,
// polygon/landmark and inline mask against current source authority.
type ImportOverrideQualifier interface {
	QualifyImportedOverride(context.Context, []byte, v2input.SessionVersionBinding) error
}

type ImportOverrideArchiveQualifier interface {
	ValidateArchivedImportOverride(context.Context, []byte) error
}

// An Override background has its own archived occurrence. The implementation
// must qualify and publish that exact image for the target before returning a
// newly registered portable ID; the StyleV2 occurrence is not interchangeable.
type ImportOverrideBackgroundRemapper interface {
	RemapImportedOverrideBackground(context.Context, v2input.Ref, string, v2input.SessionVersionBinding) (string, error)
}

// Confirmation is independent of the archived flag. Implementations must bind
// their evidence to the new asset version and recheck current consent.
type ImportConfirmedMaskAuthority interface {
	QualifyArchivedMaskConfirmation(context.Context, ImportMaskConfirmationScope, review.MaskRef, v2input.SessionVersionBinding, v2input.SessionVersionBinding) (int64, error)
	QualifyImportedMaskConfirmation(context.Context, ImportMaskConfirmationScope, review.MaskRef, review.MaskRef, v2input.SessionVersionBinding, v2input.SessionVersionBinding, int64) error
	QualifyPreparedMaskConfirmation(context.Context, review.MaskRef, v2input.SessionVersionBinding) error
}
type ImportOverrideCodec struct {
	Source      v2input.Ref
	Masters     ImportMasterReader
	Masks       ImportDependencyCodec
	Confirmed   ImportConfirmedMaskAuthority
	Archive     ImportOverrideArchiveQualifier
	Backgrounds ImportOverrideBackgroundRemapper
	Qualifier   ImportOverrideQualifier
	Fallback    ImportDependencyCodec
}

func (c ImportOverrideCodec) sourceSHA(ctx context.Context, binding v2input.SessionVersionBinding) (string, error) {
	if c.Masters == nil || !validImportRef(binding.Source) {
		return "", v2input.ErrInvalid
	}
	raw, err := c.Masters.ReadImportMaster(ctx, binding.Source, 1<<20)
	if err != nil {
		return "", err
	}
	if len(raw) < 2 || len(raw) > 1<<20 || rawSHA(raw) != binding.Source.SHA256 {
		return "", v2input.ErrInvalid
	}
	var typed review.WorkingImage
	if err := decodeImportDocument(raw, &typed); err != nil {
		return "", err
	}
	image, err := review.ParseWorkingImage(raw)
	if err != nil {
		return "", err
	}
	canvas, err := canonical.Hash(image.Canvas)
	if err != nil {
		return "", err
	}
	if canvas != binding.CanvasHash {
		return "", v2input.ErrInvalid
	}
	return image.Canvas.SourceSHA256, ctx.Err()
}
func (c ImportOverrideCodec) RemapImportDocument(ctx context.Context, contract string, raw []byte, old, target v2input.SessionVersionBinding, resolve func(v2input.Ref, string) (v2input.Ref, error)) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if contract != "Override" {
		if c.Fallback != nil {
			return c.Fallback.RemapImportDocument(ctx, contract, raw, old, target, resolve)
		}
		return nil, &ImportRecipeUnsupported{Reason: "dependency_contract_not_installed"}
	}
	if c.Qualifier == nil || resolve == nil {
		return nil, v2input.ErrInvalid
	}
	if old.CanvasHash != target.CanvasHash {
		return nil, &ImportRecipeUnsupported{Reason: "override_canvas_remap_required"}
	}
	sourceSHA, err := c.sourceSHA(ctx, target)
	if err != nil {
		return nil, err
	}
	if err := editor.ValidateOverride(raw, sourceSHA); err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	for _, name := range []string{"accepted_candidate_ids", "rejected_candidate_ids"} {
		if value, ok := fields[name]; ok {
			var ids []string
			if err := json.Unmarshal(value, &ids); err != nil {
				return nil, err
			}
			if len(ids) > 0 {
				return nil, &ImportRecipeUnsupported{Reason: "foreign_candidate_review_requires_remap"}
			}
		}
	}
	if rawModules, exists := fields["modules"]; exists {
		var modules map[string]json.RawMessage
		if err := json.Unmarshal(rawModules, &modules); err != nil || modules == nil {
			return nil, v2input.ErrInvalid
		}
		if rawExtensions, exists := modules["extensions"]; exists && string(rawExtensions) != "null" {
			var extensions map[string]json.RawMessage
			if err := json.Unmarshal(rawExtensions, &extensions); err != nil || extensions == nil {
				return nil, v2input.ErrInvalid
			}
			if rawBackground, exists := extensions["background"]; exists && string(rawBackground) != "null" {
				var background map[string]json.RawMessage
				if err := json.Unmarshal(rawBackground, &background); err != nil || background == nil {
					return nil, v2input.ErrInvalid
				}
				var mode string
				if err := json.Unmarshal(background["mode"], &mode); err != nil {
					return nil, v2input.ErrInvalid
				}
				if mode == "asset" {
					if c.Archive == nil {
						return nil, &ImportRecipeUnsupported{Reason: "override_archive_qualifier_not_installed"}
					}
					if err := c.Archive.ValidateArchivedImportOverride(ctx, append([]byte(nil), raw...)); err != nil {
						return nil, err
					}
					if c.Backgrounds == nil {
						return nil, &ImportRecipeUnsupported{Reason: "override_background_remapper_not_installed"}
					}
					var portable string
					if err := json.Unmarshal(background["asset_id"], &portable); err != nil || portable == "" || !validImportRef(c.Source) || c.Source.SHA256 != rawSHA(raw) {
						return nil, v2input.ErrInvalid
					}
					mapped, err := c.Backgrounds.RemapImportedOverrideBackground(ctx, c.Source, portable, target)
					if err != nil {
						return nil, err
					}
					if mapped == portable || mapped == "" || len(mapped) > 120 || !utf8.ValidString(mapped) || strings.TrimSpace(mapped) != mapped {
						return nil, v2input.ErrInvalid
					}
					background["asset_id"], err = canonical.Marshal(mapped)
					if err != nil {
						return nil, err
					}
					extensions["background"], err = canonical.Marshal(background)
					if err != nil {
						return nil, err
					}
					modules["extensions"], err = canonical.Marshal(extensions)
					if err != nil {
						return nil, err
					}
					fields["modules"], err = canonical.Marshal(modules)
					if err != nil {
						return nil, err
					}
				}
			}
		}
	}
	if value, ok := fields["object_id"]; ok && string(value) != "null" {
		var object string
		if err := json.Unmarshal(value, &object); err != nil {
			return nil, err
		}
		if len(object) < 1 || len(object) > 160 {
			return nil, v2input.ErrInvalid
		}
		mapped := uuid.NewSHA1(target.AssetVersionID, []byte("interchange:override-object:"+object))
		fields["object_id"], err = canonical.Marshal(mapped.String())
		if err != nil {
			return nil, err
		}
	}
	if value, ok := fields["mask_refs"]; ok {
		var masks []map[string]json.RawMessage
		if err := json.Unmarshal(value, &masks); err != nil {
			return nil, err
		}
		for _, mask := range masks {
			var confirmed bool
			if value, present := mask["confirmed"]; present {
				if err := json.Unmarshal(value, &confirmed); err != nil {
					return nil, err
				}
			}
			if confirmed && c.Confirmed == nil {
				return nil, &ImportRecipeUnsupported{Reason: "imported_mask_requires_new_confirmation"}
			}
			if c.Masks == nil {
				return nil, v2input.ErrInvalid
			}
			var archived review.MaskRef
			var confirmationScope ImportMaskConfirmationScope
			var confirmationEpoch int64
			if confirmed {
				confirmationScope, err = confirmedOverrideScope(ctx, raw)
				if err != nil {
					return nil, err
				}
				archived, err = review.ParseMaskRef(mask["mask"])
				if err != nil {
					return nil, err
				}
				if archived.Selector != "person" || archived.ObjectID != nil {
					return nil, &ImportRecipeUnsupported{Reason: "imported_mask_requires_new_confirmation"}
				}
				confirmationEpoch, err = c.Confirmed.QualifyArchivedMaskConfirmation(ctx, confirmationScope, archived, old, target)
				if err != nil {
					return nil, err
				}
				if confirmationEpoch < 1 {
					return nil, v2input.ErrInvalid
				}
			}
			maskCtx := ctx
			if confirmed {
				maskCtx = withImportConfirmedMaskScope(ctx, confirmationScope, archived)
			}
			mapped, err := c.Masks.RemapImportDocument(maskCtx, "MaskRef", mask["mask"], old, target, resolve)
			if err != nil {
				return nil, err
			}
			mask["mask"] = append([]byte(nil), mapped...)
			if confirmed {
				prepared, err := review.ParseMaskRef(mapped)
				if err != nil {
					return nil, err
				}
				if prepared.Selector != "person" || prepared.ObjectID != nil {
					return nil, &ImportRecipeUnsupported{Reason: "imported_mask_requires_new_confirmation"}
				}
				if err := c.Confirmed.QualifyImportedMaskConfirmation(ctx, confirmationScope, archived, prepared, old, target, confirmationEpoch); err != nil {
					return nil, err
				}
			}
		}
		fields["mask_refs"], err = canonical.Marshal(masks)
		if err != nil {
			return nil, err
		}
	}
	prepared, err := canonical.Marshal(fields)
	if err != nil {
		return nil, err
	}
	if err := c.QualifyRemappedImportDocument(ctx, contract, prepared, target); err != nil {
		return nil, err
	}
	return prepared, ctx.Err()
}
func (c ImportOverrideCodec) QualifyRemappedImportDocument(ctx context.Context, contract string, raw []byte, target v2input.SessionVersionBinding) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if contract != "Override" {
		if c.Fallback != nil {
			return c.Fallback.QualifyRemappedImportDocument(ctx, contract, raw, target)
		}
		return &ImportRecipeUnsupported{Reason: "dependency_contract_not_installed"}
	}
	if c.Qualifier == nil {
		return v2input.ErrInvalid
	}
	source, err := c.sourceSHA(ctx, target)
	if err != nil {
		return err
	}
	if err := editor.ValidateOverride(raw, source); err != nil {
		return err
	}
	if err := c.Qualifier.QualifyImportedOverride(ctx, append([]byte(nil), raw...), target); err != nil {
		return err
	}
	return ctx.Err()
}

func (c ImportOverrideCodec) BindImportDocumentSource(source v2input.Ref) ImportDependencyCodec {
	c.Source = source
	if binder, ok := c.Fallback.(ImportDocumentSourceBinder); ok {
		c.Fallback = binder.BindImportDocumentSource(source)
	}
	return c
}
