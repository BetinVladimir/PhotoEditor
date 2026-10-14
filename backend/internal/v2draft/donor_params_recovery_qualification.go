package v2draft

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
	"photoeditor/backend/internal/v2qualification"
)

// ReloadQualification resolves current SQL authority and then verifies the
// original report/calibration bytes under trusted operator locks. Catalog
// metadata must equal the recovery seed before any object read. No stored
// locator is used directly as IO authority or as proof of current permission.
func (s DonorParamsRecoverySeed) ReloadQualification(ctx context.Context, who auth.Identity, workflow, tenant, project uuid.UUID,
	locks v2qualification.DeploymentLocks, catalog v2portrait.DonorQualificationCatalog, objects v2input.SessionAnalysisObjectReader) (v2portrait.LocatedDonorQualification, error) {
	var zero v2portrait.LocatedDonorQualification
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if catalog == nil || objects == nil {
		return zero, v2input.ErrInvalid
	}
	raw, err := canonical.Marshal(s.intent)
	if err != nil {
		return zero, err
	}
	owned, err := ParseDonorParamsRecoveryIntent(ctx, who, workflow, raw)
	if err != nil {
		return zero, err
	}
	pinned := donorRecoveryQualificationCatalog{catalog: catalog, pin: owned.QualificationPin(), expected: owned.QualificationObjects()}
	return v2portrait.LoadDonorQualificationEvidence(ctx, pinned, objects, who, tenant, project, owned.QualificationPin(), locks)
}

type donorRecoveryQualificationCatalog struct {
	catalog  v2portrait.DonorQualificationCatalog
	pin      v2portrait.DonorQualificationPin
	expected []v2portrait.DonorQualificationObject
}

func (c donorRecoveryQualificationCatalog) ResolveDonorQualification(ctx context.Context, who auth.Identity, tenant, project uuid.UUID, pin v2portrait.DonorQualificationPin) (string, objectstore.Object, objectstore.Object, error) {
	empty := objectstore.Object{}
	if err := ctx.Err(); err != nil {
		return "", empty, empty, err
	}
	if c.catalog == nil || pin != c.pin || len(c.expected) != 2 {
		return "", empty, empty, v2input.ErrInvalid
	}
	bucket, report, calibration, err := c.catalog.ResolveDonorQualification(ctx, who, tenant, project, pin)
	if err != nil {
		return "", empty, empty, err
	}
	if err = ctx.Err(); err != nil {
		return "", empty, empty, err
	}
	for i, obj := range []objectstore.Object{calibration, report} {
		expected := c.expected[i]
		if expected.Bucket != bucket || expected.Key != obj.Key || expected.Version != obj.VersionID || expected.SHA256 != obj.SHA256 || expected.Bytes != obj.Bytes || obj.ContentType != "application/json" {
			return "", empty, empty, v2input.ErrExpired
		}
	}
	return bucket, report, calibration, nil
}
