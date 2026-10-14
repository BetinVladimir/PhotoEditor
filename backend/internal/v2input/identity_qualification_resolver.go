package v2input

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2qualification"
)

type IdentityQualificationQuery struct {
	Tenant, Project       uuid.UUID
	Backend, ModelLockSHA string
	Calibration           Ref
}

type IdentityQualificationCandidate struct {
	Pin          IdentityQualificationPin
	Expected     v2qualification.Expected
	ReportObject objectstore.Object
}

type IdentityQualificationCatalog interface {
	ResolveIdentityQualification(context.Context, IdentityQualificationQuery) (IdentityQualificationCandidate, error)
}

type SQLIdentityQualificationCatalog struct {
	DB     *pgxpool.Pool
	Bucket string
}

func (c SQLIdentityQualificationCatalog) ResolveIdentityQualification(ctx context.Context,
	query IdentityQualificationQuery) (IdentityQualificationCandidate, error) {
	var zero IdentityQualificationCandidate
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if c.DB == nil || len(c.Bucket) < 3 || len(c.Bucket) > 63 ||
		query.Tenant == uuid.Nil || query.Project == uuid.Nil ||
		(query.Backend != "cpu" && query.Backend != "cuda") ||
		!digestPattern.MatchString(query.ModelLockSHA) || !validRef(query.Calibration) {
		return zero, ErrInvalid
	}
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := c.DB.Begin(bounded)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(context.WithoutCancel(bounded))
	if _, err = tx.Exec(bounded, "SELECT set_config('app.tenant_id',$1,true)",
		query.Tenant.String()); err != nil {
		return zero, err
	}
	var role string
	if err = tx.QueryRow(bounded, "SELECT current_user").Scan(&role); err != nil {
		return zero, err
	}
	if role != "photo_scheduler" {
		return zero, ErrInvalid
	}
	var pin IdentityQualificationPin
	var checks, fixtures, calibrations, evidence []string
	var issuer uuid.UUID
	var object objectstore.Object
	err = tx.QueryRow(bounded, `SELECT policy_id,qualification_id,issuer_id,
 model_lock_sha,runtime_lock_sha,device_profile_sha,dataset_sha,backend,
 required_checks,test_fixture_shas,calibration_shas,evidence_shas,
 report_id,report_sha,object_key,object_version,bytes
 FROM photo.v23_identity_qualification_source($1,$2,$3,$4,$5,$6,$7)`,
		query.Tenant, query.Project, query.ModelLockSHA,
		query.Calibration.ArtifactID, query.Calibration.SHA256,
		query.Backend, c.Bucket).Scan(&pin.PolicyID, &pin.QualificationID, &issuer,
		&pin.ModelLockSHA, &pin.RuntimeLockSHA, &pin.DeviceProfileSHA,
		&pin.DatasetSHA, &pin.Backend, &checks, &fixtures, &calibrations,
		&evidence, &pin.Report.ArtifactID, &pin.Report.SHA256,
		&object.Key, &object.VersionID, &object.Bytes)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, ErrExpired
	}
	if err != nil {
		return zero, err
	}
	pin.Calibration = query.Calibration
	if !validIdentityQualificationPin(pin) || pin.ModelLockSHA != query.ModelLockSHA ||
		pin.Backend != query.Backend || issuer == uuid.Nil ||
		object.Key == "" || len(object.Key) > 1024 ||
		object.VersionID == "" || object.VersionID == "null" ||
		len(object.VersionID) > 1024 || object.Bytes < 2 || object.Bytes > 8<<20 {
		return zero, ErrInvalid
	}
	locks := v2qualification.DeploymentLocks{Operation: "identity.compare",
		Version: "1", Backend: pin.Backend, ModelLockSHA: pin.ModelLockSHA,
		RuntimeLockSHA: pin.RuntimeLockSHA, DeviceProfileSHA: pin.DeviceProfileSHA,
		DatasetSHA: pin.DatasetSHA, IssuerID: issuer.String(),
		RequiredChecks: checks, TestFixtureSHAs: fixtures,
		CalibrationSHAs: calibrations, EvidenceSHAs: evidence, LicenseCleared: true}
	expected, err := locks.Expected()
	if err != nil {
		return zero, ErrInvalid
	}
	if _, ok := expected.CalibrationSHAs[pin.Calibration.SHA256]; !ok {
		return zero, ErrInvalid
	}
	object.SHA256 = pin.Report.SHA256
	object.ContentType = "application/json"
	if err = tx.Commit(bounded); err != nil {
		return zero, err
	}
	return IdentityQualificationCandidate{Pin: pin, Expected: expected,
		ReportObject: object}, nil
}

// ResolveQualifiedIdentityPin reads and verifies the selected report, then
// re-resolves policy/report authority to reject rotation during object IO.
func ResolveQualifiedIdentityPin(ctx context.Context, query IdentityQualificationQuery,
	calibrationRaw []byte, catalog IdentityQualificationCatalog,
	objects SessionAnalysisObjectReader) (IdentityQualificationPin, error) {
	var zero IdentityQualificationPin
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if catalog == nil || objects == nil || query.Tenant == uuid.Nil ||
		query.Project == uuid.Nil || !validRef(query.Calibration) ||
		!digestPattern.MatchString(query.ModelLockSHA) ||
		(query.Backend != "cpu" && query.Backend != "cuda") {
		return zero, ErrInvalid
	}
	first, err := catalog.ResolveIdentityQualification(ctx, query)
	if err != nil {
		return zero, err
	}
	if !validIdentityQualificationPin(first.Pin) ||
		first.Pin.Calibration != query.Calibration ||
		first.Pin.ModelLockSHA != query.ModelLockSHA ||
		first.Pin.Backend != query.Backend ||
		first.Expected.DatasetSHA != first.Pin.DatasetSHA ||
		first.Expected.ModelLockSHA != first.Pin.ModelLockSHA ||
		first.Expected.RuntimeLockSHA != first.Pin.RuntimeLockSHA ||
		first.Expected.DeviceProfileSHA != first.Pin.DeviceProfileSHA ||
		first.Expected.Backend != first.Pin.Backend ||
		first.ReportObject.SHA256 != first.Pin.Report.SHA256 ||
		first.ReportObject.ContentType != "application/json" ||
		VerifySemanticCalibration(query.Calibration, calibrationRaw,
			first.Pin.ModelLockSHA, first.Pin.DatasetSHA) != nil {
		return zero, ErrInvalid
	}
	body, err := ReadPinnedSemanticDocument(ctx, objects,
		first.ReportObject, first.Pin.Report)
	if err != nil {
		return zero, err
	}
	if _, err = v2qualification.Verify(body, first.Pin.Report.SHA256,
		first.Expected); err != nil {
		return zero, err
	}
	second, err := catalog.ResolveIdentityQualification(ctx, query)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(first, second) {
		return zero, ErrExpired
	}
	return first.Pin, ctx.Err()
}
