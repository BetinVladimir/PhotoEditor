package v2portrait

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2qualification"
	"sort"
)

// LocalPolicySHA fingerprints trusted deployment metadata, not admission authority.
// Sorting copied sets makes policy identity independent of database array order.
func LocalPolicySHA(id uuid.UUID, locks v2qualification.DeploymentLocks) (string, error) {
	if id == uuid.Nil || (locks.Operation != "portrait.hair" && locks.Operation != "portrait.clothing") || locks.Version != "1" || locks.Backend != "cpu" {
		return "", v2qualification.ErrUnqualified
	}
	expected, err := locks.Expected()
	if err != nil {
		return "", err
	}
	sorted := func(values []string) []string {
		result := append([]string{}, values...)
		sort.Strings(result)
		return result
	}
	return canonical.Hash(map[string]any{
		"schema_version": 1, "policy_id": id.String(), "operation": locks.Operation,
		"operation_version": locks.Version, "backend": locks.Backend,
		"model_lock_sha": locks.ModelLockSHA, "runtime_lock_sha": locks.RuntimeLockSHA,
		"device_profile_sha": locks.DeviceProfileSHA, "dataset_sha": locks.DatasetSHA,
		"issuer_id": expected.IssuerID.String(), "required_checks": sorted(locks.RequiredChecks),
		"test_fixture_shas": sorted(locks.TestFixtureSHAs), "calibration_shas": sorted(locks.CalibrationSHAs),
		"evidence_shas": sorted(locks.EvidenceSHAs), "license_cleared": locks.LicenseCleared,
	})
}
