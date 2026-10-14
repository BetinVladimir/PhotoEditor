package v2input

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2qualification"
)

func SemanticAssemblyQualification(db *pgxpool.Pool, objects SessionAnalysisObjectReader, bucket string) func(context.Context, SemanticAssemblyLease, Ref, []byte, string) error {
	return func(ctx context.Context, l SemanticAssemblyLease, calibration Ref, raw []byte, model string) error {
		if db == nil || objects == nil || bucket == "" || !validSemanticAssemblyLease(l) || !validRef(calibration) || !digestPattern.MatchString(model) {
			return ErrInvalid
		}
		expected := v2qualification.Expected{Operation: "semantic.transfer", Version: "1", Backend: "cuda", ModelLockSHA: model, LicenseCleared: true}
		var fixtures, calibrations, evidence []string
		var report Ref
		var obj objectstore.Object
		var reportBucket string
		err := db.QueryRow(ctx, `SELECT dataset_sha,runtime_lock_sha,device_profile_sha,issuer_id,required_checks,test_fixture_shas,calibration_shas,evidence_shas,report_id,report_sha,bucket,object_key,object_version,bytes FROM photo.v27_semantic_frozen_assembly_qualification($1,$2,$3,$4,$5,$6,$7)`, l.TenantID, l.RunID, l.ExpansionID, l.Token, model, calibration.SHA256, calibration.ArtifactID).Scan(&expected.DatasetSHA, &expected.RuntimeLockSHA, &expected.DeviceProfileSHA, &expected.IssuerID, &expected.RequiredChecks, &fixtures, &calibrations, &evidence, &report.ArtifactID, &report.SHA256, &reportBucket, &obj.Key, &obj.VersionID, &obj.Bytes)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrExpired
		}
		if err != nil {
			return err
		}
		if reportBucket != bucket {
			return ErrInvalid
		}
		set := func(values []string) (map[string]struct{}, error) {
			if len(values) > 10000 {
				return nil, ErrInvalid
			}
			out := map[string]struct{}{}
			for _, value := range values {
				if !digestPattern.MatchString(value) {
					return nil, ErrInvalid
				}
				if _, found := out[value]; found {
					return nil, ErrInvalid
				}
				out[value] = struct{}{}
			}
			return out, nil
		}
		expected.TestFixtureSHAs, err = set(fixtures)
		if err != nil {
			return err
		}
		expected.CalibrationSHAs, err = set(calibrations)
		if err != nil {
			return err
		}
		expected.EvidenceSHAs, err = set(evidence)
		if err != nil {
			return err
		}
		if err := VerifySemanticCalibration(calibration, raw, model, expected.DatasetSHA); err != nil {
			return err
		}
		obj.SHA256 = report.SHA256
		obj.ContentType = "application/json"
		body, err := readSemanticSourceBytes(ctx, objects, obj, report)
		if err != nil {
			return err
		}
		_, err = v2qualification.Verify(body, report.SHA256, expected)
		return err
	}
}
