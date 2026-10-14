package interchange

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type ExportAdditionalWorkingAuthority interface {
	QualifyAdditionalExportWorking(context.Context, v2input.Entry, []byte) (string, error)
}
type ExportCorrectionMemberAuthority interface {
	QualifyExportCorrectionMember(context.Context, v2input.SessionVersionBinding, v2input.Ref, []byte) error
}

// This resolves dependencies of selected recipes; it never adds export entries.
// Plans must use the full producer/root validator, with nonrecursive readers.
type ExportCorrectionWorkingAuthority struct {
	Parents, Plans ExportClosureNodeResolver
	Recipes        []exportRecipeBinding
	Authority      ExportCorrectionMemberAuthority
	Fallback       ExportAdditionalWorkingAuthority
}

func (s ExportCorrectionWorkingAuthority) QualifyAdditionalExportWorking(ctx context.Context, entry v2input.Entry, raw []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if s.Parents == nil || s.Plans == nil || s.Authority == nil || entry.Contract != "WorkingImage" || !validImportRef(entry.Ref) || len(raw) < 2 || len(raw) > 8<<20 || rawSHA(raw) != entry.Ref.SHA256 {
		return "", v2input.ErrInvalid
	}
	image, err := review.ParseWorkingImage(raw)
	if err != nil {
		return "", err
	}
	canvas, err := canonical.Hash(image.Canvas)
	if err != nil {
		return "", err
	}
	found := false
	seen := map[uuid.UUID]v2input.Ref{}
	for _, expected := range s.Recipes {
		parent := v2input.Entry{Ref: expected.ref, Contract: "FrozenRenderRecipe"}
		node, err := s.Parents.ResolveExportNode(ctx, parent)
		if err != nil {
			return "", err
		}
		if node.Entry != parent || rawSHA(node.Body) != parent.Ref.SHA256 {
			return "", v2input.ErrInvalid
		}
		frozen, err := v2draft.DecodeFrozenRenderRecipe(node.Body)
		if err != nil || frozen.Scalars.Source != expected.binding {
			return "", v2input.ErrInvalid
		}
		roots, err := v2draft.FrozenRenderRecipeDependencies(node.Body, parent.Ref)
		if err != nil {
			return "", err
		}
		for _, root := range roots {
			if root.Contract != "CorrectionPlan" {
				continue
			}
			if old, exists := seen[root.Ref.ArtifactID]; exists {
				if old != root.Ref {
					return "", v2input.ErrInvalid
				}
				continue
			}
			seen[root.Ref.ArtifactID] = root.Ref
			edge := false
			for _, child := range node.Children {
				if child.Ref.ArtifactID == root.Ref.ArtifactID {
					if child.Ref != root.Ref || child.Contract != "CorrectionPlan" {
						return "", v2input.ErrInvalid
					}
					edge = true
				}
			}
			if !edge {
				return "", v2input.ErrInvalid
			}
			planEntry := v2input.Entry{Ref: root.Ref, Contract: "CorrectionPlan"}
			planNode, err := s.Plans.ResolveExportNode(ctx, planEntry)
			if err != nil {
				return "", err
			}
			if planNode.Entry != planEntry || rawSHA(planNode.Body) != root.Ref.SHA256 {
				return "", v2input.ErrInvalid
			}
			if err := v2draft.ValidateCorrectionPlan(planNode.Body, expected.binding); err != nil {
				return "", err
			}
			var plan struct {
				Items []struct {
					Binding v2input.SessionVersionBinding `json:"binding"`
				} `json:"items"`
			}
			if err := json.Unmarshal(planNode.Body, &plan); err != nil {
				return "", err
			}
			for _, item := range plan.Items {
				if item.Binding.Source.ArtifactID == entry.Ref.ArtifactID {
					if item.Binding.Source != entry.Ref || item.Binding.CanvasHash != canvas {
						return "", v2input.ErrInvalid
					}
					childFound := false
					for _, child := range planNode.Children {
						if child.Ref.ArtifactID == entry.Ref.ArtifactID {
							if child != entry {
								return "", v2input.ErrInvalid
							}
							childFound = true
						}
					}
					if !childFound {
						return "", v2input.ErrInvalid
					}
					if err := s.Authority.QualifyExportCorrectionMember(ctx, item.Binding, root.Ref, append([]byte(nil), raw...)); err != nil {
						return "", err
					}
					found = true
				}
			}
		}
	}
	if !found {
		if s.Fallback != nil {
			return s.Fallback.QualifyAdditionalExportWorking(ctx, entry, append([]byte(nil), raw...))
		}
		return "", v2input.ErrInvalid
	}
	return canvas, ctx.Err()
}
func (s SQLExportDocumentCatalog) QualifyExportCorrectionMember(ctx context.Context, binding v2input.SessionVersionBinding, parent v2input.Ref, raw []byte) error {
	return qualifyExportCorrectionMember(ctx, s, s.DB, s.Bucket, s.Request.ProjectID, s.Who, s.Tenant, binding, parent, raw)
}
func (s SQLExportResultInputCatalog) QualifyExportCorrectionMember(ctx context.Context, binding v2input.SessionVersionBinding, parent v2input.Ref, raw []byte) error {
	config := s.Export
	return qualifyExportCorrectionMember(ctx, s, config.DB, config.Bucket, config.Request.ProjectID, config.Who, config.Tenant, binding, parent, raw)
}
func qualifyExportCorrectionMember(ctx context.Context, source ExportDocumentCatalog, db *pgxpool.Pool, bucket string, project uuid.UUID, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, parent v2input.Ref, raw []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if source == nil || db == nil || project == uuid.Nil || tenantID == uuid.Nil || len(bucket) < 3 || binding.AssetVersionID == uuid.Nil || !validImportRef(parent) || !validImportRef(binding.Source) || len(raw) < 2 || len(raw) > 8<<20 || rawSHA(raw) != binding.Source.SHA256 {
		return v2input.ErrInvalid
	}
	entry := v2input.Entry{Ref: binding.Source, Contract: "WorkingImage"}
	current, err := source.LookupExportDocument(ctx, entry)
	if err != nil {
		return err
	}
	catalog := v2draft.SQLArtifactCatalog{DB: db, Bucket: bucket, ProjectID: project, AssetVersionID: binding.AssetVersionID}
	object, err := catalog.Lookup(ctx, who, tenantID, v2draft.LayerRef{Ref: binding.Source, Contract: "WorkingImage", ParentRef: parent, CanvasHash: binding.CanvasHash, ExpectedBinding: &binding})
	if err != nil {
		return err
	}
	if current != object || object.Bytes != int64(len(raw)) {
		return v2input.ErrInvalid
	}
	again, err := source.LookupExportDocument(ctx, entry)
	if err != nil {
		return err
	}
	if again != current {
		return v2input.ErrInvalid
	}
	return ctx.Err()
}

var _ ExportAdditionalWorkingAuthority = ExportCorrectionWorkingAuthority{}
var _ ExportCorrectionMemberAuthority = SQLExportDocumentCatalog{}
var _ ExportCorrectionMemberAuthority = SQLExportResultInputCatalog{}
