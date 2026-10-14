package interchange

import (
	"context"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

type ExportBodyDependencyQualification struct {
	Report v2input.Ref
	Object objectstore.Object
}

// The report and Expected policy are selected by the operator, not mask claims.
func (s SQLExportDocumentCatalog) ReadExportBodyDependencyQualification(ctx context.Context, objects ImportDocumentObjects, report v2input.Ref, expected v2qualification.Expected) (ExportBodyDependencyQualification, error) {
	var zero ExportBodyDependencyQualification
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || objects == nil || !validImportRef(report) || !exportBodyDependencyQualificationOperation(expected.Operation) || expected.Version != "1" || !expected.LicenseCleared {
		return zero, v2input.ErrInvalid
	}
	frozen, err := canonical.Marshal(expected)
	if err != nil {
		return zero, err
	}
	lookup := func() (objectstore.Object, error) {
		var out objectstore.Object
		err := tenant.WithTenant(ctx, s.DB, s.Who, s.Tenant, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
			rows, err := tx.Query(ctx, `SELECT artifact_id,sha256,bucket,object_key,object_version,bytes FROM photo.v30_body_dependency_qualification_object($1,$2,$3,$4,$5)`, s.Tenant, s.Request.ProjectID, s.Who.UserID, s.Who.SessionID, frozen)
			if err != nil {
				return err
			}
			defer rows.Close()
			found := false
			for rows.Next() {
				var ref v2input.Ref
				var bucket string
				var object objectstore.Object
				if err := rows.Scan(&ref.ArtifactID, &ref.SHA256, &bucket, &object.Key, &object.VersionID, &object.Bytes); err != nil {
					return err
				}
				if ref != report {
					continue
				}
				if found || bucket != s.Bucket || object.Key == "" || object.VersionID == "" || object.VersionID == "null" || object.Bytes < 2 || object.Bytes > 8<<20 {
					return v2input.ErrInvalid
				}
				found = true
				object.SHA256 = report.SHA256
				object.ContentType = "application/json"
				out = object
			}
			if err := rows.Err(); err != nil {
				return err
			}
			if !found {
				return v2qualification.ErrUnqualified
			}
			return ctx.Err()
		})
		if err != nil {
			return objectstore.Object{}, err
		}
		return out, ctx.Err()
	}
	before, err := lookup()
	if err != nil {
		return zero, err
	}
	body, err := v2input.ReadPinnedSemanticDocument(ctx, objects, before, report)
	if err != nil {
		return zero, err
	}
	// Restore immutable configured maps/slices before calling the verifier.
	var owned v2qualification.Expected
	if err := decodeImportDocument(frozen, &owned); err != nil {
		return zero, err
	}
	if _, err := v2qualification.Verify(body, report.SHA256, owned); err != nil {
		return zero, err
	}
	after, err := lookup()
	if err != nil {
		return zero, err
	}
	if after != before {
		return zero, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return ExportBodyDependencyQualification{Report: report, Object: after}, nil
}

func exportBodyDependencyQualificationOperation(operation string) bool {
	switch operation {
	case "pose.detect", "face.landmarks", "pair.fit.body":
		return true
	default:
		return false
	}
}
func (s SQLExportResultInputCatalog) ReadExportBodyDependencyQualification(ctx context.Context, objects ImportDocumentObjects, report v2input.Ref, expected v2qualification.Expected) (ExportBodyDependencyQualification, error) {
	config, err := exportPolicyCatalogConfig(s)
	if err != nil {
		return ExportBodyDependencyQualification{}, err
	}
	return config.ReadExportBodyDependencyQualification(ctx, objects, report, expected)
}
