package v2render

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

type SQLAuthoredSkinQualification struct {
	API     *pgxpool.Pool
	Objects v2input.SessionAnalysisObjectReader
	Bucket  string
}
type authoredSkinQualification struct {
	expected string
	ref      v2input.Ref
	object   objectstore.Object
	raw      []byte
	report   v2qualification.Report
}

func (s SQLAuthoredSkinQualification) locator(ctx context.Context, who auth.Identity, source AuthoredInstructionSource, expected v2qualification.Expected, ref v2input.Ref) (objectstore.Object, error) {
	var out objectstore.Object
	if s.API == nil || s.Objects == nil || s.Bucket == "" || expected.Operation != "mask.face_skin" || expected.Version != "1" || expected.Backend != "cpu" || !validLayerArtifactRef(ref) {
		return out, v2qualification.ErrUnqualified
	}
	body, err := canonical.Marshal(expected)
	if err != nil {
		return out, err
	}
	err = tenant.WithTenant(ctx, s.API, who, source.Tenant, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		rows, e := tx.Query(ctx, "SELECT artifact_id,sha256,bucket,object_key,object_version,bytes FROM photo.authored_skin_qualification_object($1,$2,$3,$4,$5)", source.Tenant, source.Project, who.UserID, who.SessionID, body)
		if e != nil {
			return e
		}
		defer rows.Close()
		found := false
		for rows.Next() {
			var actual v2input.Ref
			var bucket string
			var obj objectstore.Object
			if e = rows.Scan(&actual.ArtifactID, &actual.SHA256, &bucket, &obj.Key, &obj.VersionID, &obj.Bytes); e != nil {
				return e
			}
			if actual != ref {
				continue
			}
			if found || bucket != s.Bucket || obj.Key == "" || obj.VersionID == "" || obj.VersionID == "null" || obj.Bytes < 2 || obj.Bytes > 8<<20 {
				return v2qualification.ErrUnqualified
			}
			found = true
			obj.SHA256 = ref.SHA256
			obj.ContentType = "application/json"
			out = obj
		}
		if e = rows.Err(); e != nil {
			return e
		}
		if !found {
			return v2qualification.ErrUnqualified
		}
		return nil
	})
	return out, err
}
func (s SQLAuthoredSkinQualification) read(ctx context.Context, who auth.Identity, source AuthoredInstructionSource, expected v2qualification.Expected, ref v2input.Ref) (authoredSkinQualification, error) {
	var zero authoredSkinQualification
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	expected = ownAuthoredDefectExpected(expected)
	before, err := s.locator(ctx, who, source, expected, ref)
	if err != nil {
		return zero, err
	}
	raw, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, before, ref)
	if err != nil {
		return zero, err
	}
	report, err := v2qualification.Verify(raw, ref.SHA256, expected)
	if err != nil {
		return zero, err
	}
	if report.Metrics.PeakVRAMBytes != 0 {
		return zero, v2qualification.ErrUnqualified
	}
	after, err := s.locator(ctx, who, source, expected, ref)
	if err != nil {
		return zero, err
	}
	if before != after {
		return zero, v2input.ErrExpired
	}
	body, err := canonical.Marshal(expected)
	if err != nil {
		return zero, err
	}
	return authoredSkinQualification{expected: string(body), ref: ref, object: before, raw: append([]byte(nil), raw...), report: report}, ctx.Err()
}
