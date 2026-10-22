package v2render

import (
	"bytes"
	"context"
	"encoding/json"
	"math"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// The proof owns actual physical report evidence and deployment policy. It is
// not constructible from report claims, an old adopted task, or a caller callback.
type AuthoredGeometryNativeQualification struct {
	tenant            uuid.UUID
	request, expected string
	reportJCS         string

	report v2input.Ref
	object objectstore.Object
	costs  v2input.SemanticQualifiedCosts
}
type SQLAuthoredGeometryNativeQualification struct {
	Scheduler *pgxpool.Pool
	Objects   v2input.SessionAnalysisObjectReader
	Bucket    string
}

func freezeGeometryNativeExpected(expected v2qualification.Expected) (v2qualification.Expected, []byte, error) {
	raw, err := canonical.Marshal(expected)
	if err != nil {
		return v2qualification.Expected{}, nil, err
	}
	var owned v2qualification.Expected
	if err = json.Unmarshal(raw, &owned); err != nil {
		return owned, nil, err
	}
	if owned.Operation != "image.upscale" || owned.Version != "1" || (owned.Backend != "cpu" && owned.Backend != "cuda" && owned.Backend != "mps") || !owned.LicenseCleared {
		return owned, nil, v2qualification.ErrUnqualified
	}
	return owned, raw, nil
}
func geometryNativeMeasuredCosts(m v2qualification.Metrics) (v2input.SemanticQualifiedCosts, error) {
	for _, n := range []float64{m.PeakRSSBytes, m.PeakVRAMBytes, m.ScratchBytes} {
		if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 9007199254740991 {
			return v2input.SemanticQualifiedCosts{}, v2qualification.ErrUnqualified
		}
	}
	return v2input.SemanticQualifiedCosts{RAMBytes: int64(math.Ceil(m.PeakRSSBytes)), VRAMBytes: int64(math.Ceil(m.PeakVRAMBytes)), ScratchBytes: int64(math.Ceil(m.ScratchBytes))}, nil
}
func (s SQLAuthoredGeometryNativeQualification) lookup(ctx context.Context, tenant uuid.UUID, request, expected []byte, report v2input.Ref) (objectstore.Object, error) {
	var object objectstore.Object
	if err := ctx.Err(); err != nil {
		return object, err
	}
	if s.Scheduler == nil || s.Objects == nil || s.Bucket == "" || tenant == uuid.Nil || report.ArtifactID == uuid.Nil {
		return object, v2qualification.ErrUnqualified
	}
	err := database.TenantTransaction(ctx, s.Scheduler, tenant, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_scheduler" {
			return v2qualification.ErrUnqualified
		}
		rows, err := tx.Query(ctx, `SELECT artifact_id,sha256,bucket,object_key,object_version,bytes FROM photo.read_authored_geometry_native_qualification($1,$2,$3,$4,$5)`, tenant, request, expected, report.ArtifactID, report.SHA256)
		if err != nil {
			return err
		}
		defer rows.Close()
		count := 0
		for rows.Next() {
			var ref v2input.Ref
			var bucket string
			var candidate objectstore.Object
			if err = rows.Scan(&ref.ArtifactID, &ref.SHA256, &bucket, &candidate.Key, &candidate.VersionID, &candidate.Bytes); err != nil {
				return err
			}
			count++
			if count != 1 || ref != report || bucket != s.Bucket || candidate.Key == "" || candidate.VersionID == "" || candidate.VersionID == "null" || candidate.Bytes < 2 || candidate.Bytes > 8<<20 {
				return v2qualification.ErrUnqualified
			}
			candidate.SHA256 = ref.SHA256
			candidate.ContentType = "application/json"
			object = candidate
		}
		if err = rows.Err(); err != nil {
			return err
		}
		if count != 1 {
			return v2qualification.ErrUnqualified
		}
		return ctx.Err()
	})
	return object, err
}
func (s SQLAuthoredGeometryNativeQualification) Read(ctx context.Context, tenant uuid.UUID, request []byte, expected v2qualification.Expected, report v2input.Ref) (AuthoredGeometryNativeQualification, error) {
	empty := AuthoredGeometryNativeQualification{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	owned, policy, err := freezeGeometryNativeExpected(expected)
	if err != nil {
		return empty, err
	}
	frozen, err := canonical.Bytes(request)
	if err != nil || !bytes.Equal(frozen, request) {
		return empty, v2input.ErrInvalid
	}
	before, err := s.lookup(ctx, tenant, frozen, policy, report)
	if err != nil {
		return empty, err
	}
	raw, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, before, report)
	if err != nil {
		return empty, err
	}
	costs, err := verifiedAuthoredNativeCosts(raw, report.SHA256, owned)
	if err != nil {
		return empty, err
	}
	after, err := s.lookup(ctx, tenant, frozen, policy, report)
	if err != nil {
		return empty, err
	}
	if before != after {
		return empty, v2input.ErrConflict
	}
	if err = ctx.Err(); err != nil {
		return empty, err
	}
	return AuthoredGeometryNativeQualification{tenant: tenant, request: string(frozen), expected: string(policy), report: report, object: before, costs: costs, reportJCS: string(raw)}, nil
}
