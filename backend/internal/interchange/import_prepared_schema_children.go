package interchange

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"regexp"
	"strconv"
)

var importedOverrideMaskPath = regexp.MustCompile(`^Document\.mask_refs\[(0|[1-9][0-9]*)\]\.mask\.(artifact|transform_chain\.steps\[(0|[1-9][0-9]*)\]\.inverse_field)$`)

// Structural role projection only. ImportCodecDocumentValidator must perform
// full installed source/model/operator semantics before invoking this adapter.
type ImportPreparedSchemaChildren struct{}

func (ImportPreparedSchemaChildren) ImportDocumentChildren(ctx context.Context, entry v2input.Entry, raw []byte) ([]v2input.Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !validImportRef(entry.Ref) || len(raw) < 2 || len(raw) > 8<<20 || rawSHA(raw) != entry.Ref.SHA256 {
		return nil, v2input.ErrInvalid
	}
	body := append([]byte(nil), raw...)
	normalized, err := canonical.Bytes(body)
	if err != nil || !bytes.Equal(body, normalized) {
		return nil, v2input.ErrInvalid
	}
	if isImportStatsParams(entry.Contract, body) {
		p, err := decodeImportStatsParams(body)
		if err != nil {
			return nil, err
		}
		maskRef := v2input.Ref{ArtifactID: uuid.NewSHA1(entry.Ref.ArtifactID, []byte("interchange:stats-mask-projection")), SHA256: rawSHA(p.Person)}
		maskChildren, err := (preparedMaskGeometryValidator{}).ImportDocumentChildren(ctx, v2input.Entry{Ref: maskRef, Contract: "MaskRef"}, p.Person)
		if err != nil {
			return nil, err
		}
		children := []v2input.Entry{{Ref: p.Image, Contract: "WorkingImage"}}
		seen := map[uuid.UUID]v2input.Entry{p.Image.ArtifactID: children[0]}
		for _, child := range maskChildren {
			if child.Ref.ArtifactID == entry.Ref.ArtifactID {
				return nil, v2input.ErrInvalid
			}
			if old, ok := seen[child.Ref.ArtifactID]; ok {
				if old != child {
					return nil, v2input.ErrInvalid
				}
				continue
			}
			seen[child.Ref.ArtifactID] = child
			children = append(children, child)
		}
		if p.Image.ArtifactID == entry.Ref.ArtifactID {
			return nil, v2input.ErrInvalid
		}
		return children, ctx.Err()
	}
	if entry.Contract == "ResolvedStyle" {
		return exportResolvedChildren(entry, body)
	}
	var refs []v2draft.LayerRef
	switch entry.Contract {
	case "LayerStack", "CorrectionPlan", "MaskRef", "Params", "Stats20", "DefectCandidates", "DefectReview", "DustCandidates":
		refs, err = v2draft.DocumentChildren(body, entry.Contract, entry.Ref)
	case "StyleV2", "Override":
		refs, err = v2draft.OpaqueDocumentChildren(body, entry.Contract, entry.Ref)
	default:
		return nil, &ImportRecipeUnsupported{Reason: "dependency_contract_not_installed"}
	}
	if err != nil {
		return nil, err
	}
	children := []v2input.Entry{}
	seen := map[uuid.UUID]v2input.Entry{}
	for _, ref := range refs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		contract := ref.Contract
		if entry.Contract == "StyleV2" {
			return nil, &ImportRecipeUnsupported{Reason: "style_reference_role_not_installed"}
		}
		if entry.Contract == "Override" {
			match := importedOverrideMaskPath.FindStringSubmatch(ref.Path)
			if match == nil {
				return nil, &ImportRecipeUnsupported{Reason: "override_reference_role_not_installed"}
			}
			index, e := strconv.Atoi(match[1])
			if e != nil || index >= 128 {
				return nil, v2input.ErrInvalid
			}
			if match[2] == "artifact" {
				contract = "MaskPixels"
			} else {
				step, e := strconv.Atoi(match[3])
				if e != nil || step >= 128 {
					return nil, v2input.ErrInvalid
				}
				contract = "DenseWarpField"
			}
		}
		// Generic nestedRefs labels unknown fields Artifact. Such a label is not
		// authority to use an opaque archive object in an executable recipe.
		if contract == "Artifact" || contract == "" {
			return nil, &ImportRecipeUnsupported{Reason: "dependency_reference_role_not_installed"}
		}
		child := v2input.Entry{Ref: ref.Ref, Contract: contract}
		if !validImportRef(child.Ref) || child.Ref.ArtifactID == entry.Ref.ArtifactID {
			return nil, v2input.ErrInvalid
		}
		if old, ok := seen[child.Ref.ArtifactID]; ok {
			if old != child {
				return nil, v2input.ErrInvalid
			}
			continue
		}
		seen[child.Ref.ArtifactID] = child
		children = append(children, child)
	}
	return children, ctx.Err()
}
