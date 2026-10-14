package v2portrait

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2qualification"
	"sort"
)

// DonorPolicySHA matches v28_donor_policy_sha's immutable expectation document.
// Enabled/admission flags remain live SQL checks, outside this content digest.
func DonorPolicySHA(policyID uuid.UUID, locks v2qualification.DeploymentLocks) (string, error) {
	if policyID == uuid.Nil {
		return "", v2qualification.ErrUnqualified
	}
	expected, err := locks.Expected()
	if err != nil {
		return "", err
	}
	if expected.Operation != "portrait.open_eyes" || expected.Version != "1" || expected.Backend != "cpu" {
		return "", v2qualification.ErrUnqualified
	}
	sorted := func(values []string) []string {
		result := append([]string{}, values...)
		sort.Strings(result)
		return result
	}
	return canonical.Hash(map[string]any{"schema_version": 1, "policy_id": policyID, "operation": expected.Operation, "operation_version": expected.Version,
		"backend": expected.Backend, "model_lock_sha": expected.ModelLockSHA, "runtime_lock_sha": expected.RuntimeLockSHA, "device_profile_sha": expected.DeviceProfileSHA, "dataset_sha": expected.DatasetSHA,
		"issuer_id": expected.IssuerID, "required_checks": sorted(locks.RequiredChecks), "test_fixture_shas": sorted(locks.TestFixtureSHAs), "calibration_shas": sorted(locks.CalibrationSHAs), "evidence_shas": sorted(locks.EvidenceSHAs), "license_cleared": expected.LicenseCleared})
}
