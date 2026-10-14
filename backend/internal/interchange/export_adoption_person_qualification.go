package interchange

import (
	"context"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

type ExportPersonQualification struct {
	Report v2input.Ref
	Object objectstore.Object
}

// ReadExportPersonQualification verifies the operator-selected report under
// the export lease, including pinned bytes and the current admission policy.
func (s SQLSchedulerExportInputCatalog) ReadExportPersonQualification(ctx context.Context,
	objects ImportDocumentObjects, report v2input.Ref,
	expected v2qualification.Expected) (ExportPersonQualification, error) {
	var zero ExportPersonQualification
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.Claims.DB == nil || s.Claim.validate() != nil || objects == nil ||
		!validImportRef(report) || expected.Operation != "mask.person" ||
		expected.Version != "1" || !expected.LicenseCleared {
		return zero, v2input.ErrInvalid
	}
	frozen, err := canonical.Marshal(expected)
	if err != nil {
		return zero, err
	}
	lookup := func() (objectstore.Object, error) {
		rows, err := s.Claims.DB.Query(ctx, `SELECT bucket,object_key,object_version,
 sha256,bytes,media_type FROM photo.v30_export_adoption_person_qualification_object(
 $1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11,$12)`,
			s.Claim.Tenant, s.Claim.Workflow, s.Claim.Run, s.Claim.Task,
			s.Claim.Result, s.Claim.Actor, s.Claim.Key, s.Claim.RequestSHA,
			s.Claim.Token, string(frozen), report.ArtifactID, report.SHA256)
		if err != nil {
			return objectstore.Object{}, err
		}
		defer rows.Close()
		var object objectstore.Object
		found := false
		for rows.Next() {
			var current objectstore.Object
			var bucket string
			if err := rows.Scan(&bucket, &current.Key, &current.VersionID,
				&current.SHA256, &current.Bytes, &current.ContentType); err != nil {
				return objectstore.Object{}, err
			}
			if found || bucket != s.Bucket || current.Key == "" ||
				current.VersionID == "" || current.VersionID == "null" ||
				current.SHA256 != report.SHA256 ||
				current.ContentType != "application/json" ||
				current.Bytes < 2 || current.Bytes > 8<<20 {
				return objectstore.Object{}, v2input.ErrInvalid
			}
			object = current
			found = true
		}
		if err := rows.Err(); err != nil {
			return objectstore.Object{}, err
		}
		if !found {
			return objectstore.Object{}, v2qualification.ErrUnqualified
		}
		return object, ctx.Err()
	}
	before, err := lookup()
	if err != nil {
		return zero, err
	}
	body, err := v2input.ReadPinnedSemanticDocument(ctx, objects, before, report)
	if err != nil {
		return zero, err
	}
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
	return ExportPersonQualification{Report: report, Object: after}, ctx.Err()
}
