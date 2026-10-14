package interchange

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// Rebuilds the label verifier source from a current prepared parent and SQL
// occurrence. A remembered parent alone grants no access to binary labels.
type SQLPreparedDefectLabelSource struct {
	Catalog   SQLPreparedImportCatalog
	Witnesses *ImportPreparedDefectLabelWitnesses
	Documents ImportClosureNodeResolver
}

func (s SQLPreparedDefectLabelSource) ResolveExportDefectLabels(ctx context.Context, entry v2input.Entry) ([]review.VerifiedProposal, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if entry.Contract != "DefectLabelMap" || !validImportRef(entry.Ref) || s.Witnesses == nil || s.Documents == nil || s.Catalog.Store.DB == nil {
		return nil, v2input.ErrInvalid
	}
	witnesses := s.Witnesses.lookup(entry.Ref)
	if len(witnesses) == 0 {
		return nil, v2input.ErrInvalid
	}
	before, err := s.Catalog.LookupImportDocument(ctx, entry)
	if err != nil {
		return nil, err
	}
	store, scope := s.Catalog.Store, s.Catalog.Scope
	parsed := append([]byte(nil), store.Parsed...)
	binding, err := canonical.Marshal(scope.Binding)
	if err != nil {
		return nil, err
	}
	proposals := make([]review.VerifiedProposal, 0, len(witnesses))
	for _, witness := range witnesses {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		parent := v2input.Entry{Ref: witness.Parent, Contract: "DefectCandidates"}
		node, err := s.Documents.ResolveImportNode(ctx, parent)
		if err != nil {
			return nil, err
		}
		if node.Entry != parent || rawSHA(node.Body) != parent.Ref.SHA256 {
			return nil, v2input.ErrInvalid
		}
		var projection struct {
			Image  v2input.Ref `json:"image"`
			Labels v2input.Ref `json:"label_map"`
		}
		if err := json.Unmarshal(node.Body, &projection); err != nil || projection.Image != scope.Binding.Source || projection.Labels != entry.Ref {
			return nil, v2input.ErrInvalid
		}
		found := 0
		for _, child := range node.Children {
			if child.Ref.ArtifactID == entry.Ref.ArtifactID {
				if child != entry {
					return nil, v2input.ErrInvalid
				}
				found++
			}
		}
		if found != 1 {
			return nil, v2input.ErrInvalid
		}
		verified, err := review.ParseDefectProposalStructure(ctx, node.Body, review.ArtifactRef{ID: parent.Ref.ArtifactID, SHA256: parent.Ref.SHA256})
		if err != nil {
			return nil, err
		}
		labelBinding, err := verified.LabelBinding()
		if err != nil || labelBinding.Width != witness.Width || labelBinding.Height != witness.Height {
			return nil, v2input.ErrInvalid
		}
		var raw []byte
		err = database.TenantTransaction(ctx, store.DB, scope.Tenant, func(tx pgx.Tx) error {
			if err := store.checkPreparedRun(ctx, tx); err != nil {
				return err
			}
			return tx.QueryRow(ctx, `SELECT photo.v30_interchange_prepared_defect_label_geometry($1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9)`, scope.Tenant, scope.Workflow, string(parsed), scope.Entry, string(binding), parent.Ref.ArtifactID, parent.Ref.SHA256, entry.Ref.ArtifactID, entry.Ref.SHA256).Scan(&raw)
		})
		if err != nil {
			return nil, err
		}
		var occurrence ImportDefectLabelOccurrence
		if err := decodeImportDocument(raw, &occurrence); err != nil {
			return nil, err
		}
		if occurrence.Width != witness.Width || occurrence.Height != witness.Height || occurrence.Original.SHA256 != entry.Ref.SHA256 {
			return nil, v2input.ErrInvalid
		}
		checked := scope
		checked.Contract = "DefectLabelMap"
		checked.DefectLabel = &occurrence
		if err := validateImportDefectLabelOccurrence(checked, entry.Ref); err != nil {
			return nil, err
		}
		proposals = append(proposals, verified)
	}
	after, err := s.Catalog.LookupImportDocument(ctx, entry)
	if err != nil || after != before {
		return nil, v2input.ErrInvalid
	}
	return proposals, ctx.Err()
}

func NewSQLPreparedImportDefectLabelResolver(ctx context.Context, catalog SQLPreparedImportCatalog, objects ImportDocumentObjects, witnesses *ImportPreparedDefectLabelWitnesses, documents ImportClosureNodeResolver, maxBytes int64) (ImportPreparedBinaryNodeResolver, error) {
	var zero ImportPreparedBinaryNodeResolver
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if catalog.Store.DB == nil || objects == nil || witnesses == nil || documents == nil || maxBytes < 1 || maxBytes > 128<<30 {
		return zero, v2input.ErrInvalid
	}
	catalog.Store.Parsed = append([]byte(nil), catalog.Store.Parsed...)
	source := SQLPreparedDefectLabelSource{Catalog: catalog, Witnesses: witnesses, Documents: documents}
	return ImportPreparedBinaryNodeResolver{Catalog: catalog, Objects: objects, Qualifier: ExportDefectLabelQualifier{Objects: objects, Source: source}, MaxBytes: maxBytes}, nil
}

var _ ExportDefectLabelSource = SQLPreparedDefectLabelSource{}
