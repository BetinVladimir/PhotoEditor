package v2render

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"math"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/facequalification"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// This adapter reuses the ordinary source-independent trusted qualification
// report reader. It does NOT reuse the master-only face observation authority.
type SQLAuthoredFaceQualification struct {
	API     *pgxpool.Pool
	Objects v2input.SessionAnalysisObjectReader
	Bucket  string
}
type AuthoredFaceQualification struct {
	tenant, project, actor, session uuid.UUID
	expected                        string
	report                          v2input.Ref
	costs                           v2input.SemanticQualifiedCosts
	object                          objectstore.Object
}

func authoredFaceMeasuredCosts(metrics v2qualification.Metrics) (v2input.SemanticQualifiedCosts, error) {
	values := []float64{metrics.PeakRSSBytes, metrics.PeakVRAMBytes, metrics.ScratchBytes}
	for _, n := range values {
		if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 9007199254740991 {
			return v2input.SemanticQualifiedCosts{}, v2input.ErrInvalid
		}
	}
	// The native face model compilers require an actual positive model-memory
	// observation. No zero or sampled allocation is invented for MPS here.
	if metrics.PeakVRAMBytes <= 0 {
		return v2input.SemanticQualifiedCosts{}, v2qualification.ErrUnqualified
	}
	return v2input.SemanticQualifiedCosts{RAMBytes: int64(math.Ceil(values[0])), VRAMBytes: int64(math.Ceil(values[1])), ScratchBytes: int64(math.Ceil(values[2]))}, nil
}
func (s SQLAuthoredFaceQualification) Read(ctx context.Context, who auth.Identity, tenant, project uuid.UUID, expected v2qualification.Expected, report v2input.Ref) (AuthoredFaceQualification, error) {
	var zero AuthoredFaceQualification
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	expected = ownAuthoredDefectExpected(expected)
	if s.API == nil || s.Objects == nil || s.Bucket == "" || tenant == uuid.Nil || project == uuid.Nil || !validLayerArtifactRef(report) || expected.Version != "1" || (expected.Backend != "cuda" && expected.Backend != "mps") {
		return zero, v2qualification.ErrUnqualified
	}
	switch expected.Operation {
	case "face.detect", "face.landmarks", "face.parse":
	default:
		return zero, v2qualification.ErrUnqualified
	}
	before, err := s.locator(ctx, who, tenant, project, expected, report)
	if err != nil {
		return zero, err
	}
	reader := facequalification.Reader{DB: s.API, Objects: s.Objects, Bucket: s.Bucket, ProjectID: project}
	verified, err := reader.VerifyTyped(ctx, who, tenant, report, expected)
	if err != nil {
		return zero, err
	}
	costs, err := authoredFaceResourceCosts(verified.Resources())
	if err != nil {
		return zero, err
	}
	after, err := s.locator(ctx, who, tenant, project, expected, report)
	if err != nil {
		return zero, err
	}
	if before != after {
		return zero, v2input.ErrConflict
	}
	body, err := canonical.Marshal(expected)
	if err != nil {
		return zero, err
	}
	return AuthoredFaceQualification{tenant: tenant, project: project, actor: who.UserID, session: who.SessionID, expected: string(body), report: report, costs: costs, object: before}, ctx.Err()
}
func (s SQLAuthoredFaceQualification) Recheck(ctx context.Context, who auth.Identity, tenant, project uuid.UUID, expected v2qualification.Expected, proof AuthoredFaceQualification) error {
	expected = ownAuthoredDefectExpected(expected)
	wire, err := canonical.Marshal(expected)
	if err != nil {
		return err
	}
	if proof.tenant != tenant || proof.project != project || proof.actor != who.UserID || proof.session != who.SessionID || proof.expected != string(wire) {
		return v2input.ErrInvalid
	}
	fresh, err := s.Read(ctx, who, tenant, project, expected, proof.report)
	if err != nil {
		return err
	}
	if fresh != proof {
		return v2input.ErrConflict
	}
	return ctx.Err()
}

func authoredFaceResourceCosts(r v2qualification.ResourceEvidence)(v2input.SemanticQualifiedCosts,error){
 if !r.Valid()||r.GPUBytes<=0{return v2input.SemanticQualifiedCosts{},v2qualification.ErrUnqualified}
 return v2input.SemanticQualifiedCosts{RAMBytes:r.RAMBytes,VRAMBytes:r.GPUBytes,ScratchBytes:r.ScratchBytes},nil
}
