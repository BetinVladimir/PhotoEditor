package interchange

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

type SQLPreparedImportLUTGeometry struct {
	Catalog   SQLPreparedImportCatalog
	Witnesses *ImportPreparedLUTWitnesses
	Documents ImportClosureNodeResolver
}

func (s SQLPreparedImportLUTGeometry) ResolveExportArrayGeometry(ctx context.Context, entry v2input.Entry) (ExportArrayGeometry, error) {
	var zero ExportArrayGeometry
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if entry.Contract != "AbsoluteLUTArray" || !validImportRef(entry.Ref) || s.Witnesses == nil || s.Documents == nil || s.Catalog.Store.DB == nil {
		return zero, v2input.ErrInvalid
	}
	witnesses := s.Witnesses.lookup(entry.Ref)
	if len(witnesses) == 0 {
		return zero, v2input.ErrInvalid
	}
	before, err := s.Catalog.LookupImportDocument(ctx, entry)
	if err != nil {
		return zero, err
	}
	store, scope := s.Catalog.Store, s.Catalog.Scope
	parsed := append([]byte(nil), store.Parsed...)
	binding, err := canonical.Marshal(scope.Binding)
	if err != nil {
		return zero, err
	}
	for _, witness := range witnesses {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		parent := v2input.Entry{Ref: witness.Parent, Contract: "ResolvedStyle"}
		node, err := s.Documents.ResolveImportNode(ctx, parent)
		if err != nil {
			return zero, err
		}
		if node.Entry != parent || rawSHA(node.Body) != parent.Ref.SHA256 {
			return zero, v2input.ErrInvalid
		}
		var projection struct {
			Params struct {
				Image v2input.Ref `json:"image"`
			} `json:"params"`
			LUT *struct {
				Artifact v2input.Ref `json:"artifact"`
				Size     int         `json:"size"`
				DType    string      `json:"dtype"`
				Order    string      `json:"order"`
				Profile  string      `json:"profile_id"`
			} `json:"absolute_lut"`
		}
		if err := json.Unmarshal(node.Body, &projection); err != nil {
			return zero, err
		}
		if projection.Params.Image != scope.Binding.Source || projection.LUT == nil || projection.LUT.Artifact != entry.Ref {
			return zero, v2input.ErrInvalid
		}
		found := false
		for _, child := range node.Children {
			if child == entry {
				found = true
			}
		}
		if !found {
			return zero, v2input.ErrInvalid
		}
		digest, err := canonical.Hash(projection.LUT)
		if err != nil {
			return zero, err
		}
		if witness.Geometry != (ExportArrayGeometry{Width: projection.LUT.Size, Height: projection.LUT.Size, CanvasSHA: digest, DType: "float32"}) {
			return zero, v2input.ErrInvalid
		}
		var raw []byte
		err = database.TenantTransaction(ctx, store.DB, scope.Tenant, func(tx pgx.Tx) error {
			if err := store.checkPreparedRun(ctx, tx); err != nil {
				return err
			}
			return tx.QueryRow(ctx, `SELECT photo.v30_interchange_prepared_lut_geometry($1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9)`, scope.Tenant, scope.Workflow, string(parsed), scope.Entry, string(binding), parent.Ref.ArtifactID, parent.Ref.SHA256, entry.Ref.ArtifactID, entry.Ref.SHA256).Scan(&raw)
		})
		if err != nil {
			return zero, err
		}
		var occurrence ImportLUTOccurrence
		if err := decodeImportDocument(raw, &occurrence); err != nil {
			return zero, err
		}
		if occurrence.Size != witness.Geometry.Width || occurrence.Original.SHA256 != entry.Ref.SHA256 || !validImportRef(occurrence.Parent) {
			return zero, v2input.ErrInvalid
		}
		checked := scope
		checked.Contract = "AbsoluteLUTArray"
		checked.LUT = &occurrence
		if err := validateImportLUTOccurrence(checked, entry.Ref); err != nil {
			return zero, err
		}
		after, err := s.Catalog.LookupImportDocument(ctx, entry)
		if err != nil {
			return zero, err
		}
		if after != before {
			return zero, v2input.ErrInvalid
		}
		return witness.Geometry, ctx.Err()
	}
	return zero, v2input.ErrInvalid
}

func NewSQLPreparedImportLUTResolver(ctx context.Context, catalog SQLPreparedImportCatalog, objects ImportDocumentObjects, witnesses *ImportPreparedLUTWitnesses, documents ImportClosureNodeResolver, scratch *ExportArchiveScratch, probe ExportNativeProbe, runtime string, maxBytes int64) (ImportPreparedBinaryNodeResolver, error) {
	var zero ImportPreparedBinaryNodeResolver
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if catalog.Store.DB == nil || objects == nil || witnesses == nil || documents == nil || scratch == nil || probe == nil || !validImportRefForSHA(runtime) || maxBytes < 1 || maxBytes > 128<<30 {
		return zero, v2input.ErrInvalid
	}
	catalog.Store.Parsed = append([]byte(nil), catalog.Store.Parsed...)
	geometry := SQLPreparedImportLUTGeometry{Catalog: catalog, Witnesses: witnesses, Documents: documents}
	qualifier := NativeExportArrayQualifier{Objects: objects, Scratch: scratch, Probe: probe, RuntimeSHA: runtime, Geometry: geometry}
	return ImportPreparedBinaryNodeResolver{Catalog: catalog, Objects: objects, Qualifier: qualifier, MaxBytes: maxBytes}, nil
}

var _ ExportArrayGeometrySource = SQLPreparedImportLUTGeometry{}
