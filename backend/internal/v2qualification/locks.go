package v2qualification

import (
	"bytes"
	"encoding/json"
	"io"
	"os"

	"github.com/google/uuid"
)

// DeploymentLocks is provisioned with the DBA qualification policy. The CLI
// reads this local file; ordinary API requests never supply these values.
type DeploymentLocks struct {
	Operation        string   `json:"operation"`
	Version          string   `json:"version"`
	Backend          string   `json:"backend"`
	ModelLockSHA     string   `json:"model_lock_sha"`
	RuntimeLockSHA   string   `json:"runtime_lock_sha"`
	DeviceProfileSHA string   `json:"device_profile_sha"`
	DatasetSHA       string   `json:"dataset_sha"`
	IssuerID         string   `json:"issuer_id"`
	RequiredChecks   []string `json:"required_checks"`
	CalibrationSHAs  []string `json:"calibration_shas"`
	TestFixtureSHAs  []string `json:"test_fixture_shas"`
	EvidenceSHAs     []string `json:"evidence_shas"`
	LicenseCleared   bool     `json:"license_cleared"`
}

// Expected validates and owns an in-memory trusted policy using the same rules
// as the deployment lock-file loader. Callers must independently authenticate it.
func (locks DeploymentLocks) Expected() (Expected, error) {
	locks.RequiredChecks = append([]string(nil), locks.RequiredChecks...)
	locks.CalibrationSHAs = append([]string(nil), locks.CalibrationSHAs...)
	locks.TestFixtureSHAs = append([]string(nil), locks.TestFixtureSHAs...)
	locks.EvidenceSHAs = append([]string(nil), locks.EvidenceSHAs...)
	return locks.expected()
}

func trustedSet(values []string, min int) (map[string]struct{}, bool) {
	if len(values) < min || len(values) > 10000 {
		return nil, false
	}
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !digest(value) {
			return nil, false
		}
		if _, found := set[value]; found {
			return nil, false
		}
		set[value] = struct{}{}
	}
	return set, true
}

func (locks DeploymentLocks) expected() (Expected, error) {
	var result Expected
	issuer, err := uuid.Parse(locks.IssuerID)
	if err != nil || issuer == uuid.Nil ||
		len(locks.Operation) < 1 || len(locks.Operation) > 80 ||
		len(locks.Version) < 1 || len(locks.Version) > 20 ||
		(locks.Backend != "cpu" && locks.Backend != "cuda" && locks.Backend != "mps") ||
		!digest(locks.ModelLockSHA) || !digest(locks.RuntimeLockSHA) ||
		!digest(locks.DeviceProfileSHA) || !digest(locks.DatasetSHA) ||
		!locks.LicenseCleared || len(locks.RequiredChecks) < 1 ||
		len(locks.RequiredChecks) > 1000 {
		return result, ErrUnqualified
	}
	checks := make(map[string]struct{}, len(locks.RequiredChecks))
	for _, check := range locks.RequiredChecks {
		if len(check) < 1 || len(check) > 100 {
			return result, ErrUnqualified
		}
		if _, found := checks[check]; found {
			return result, ErrUnqualified
		}
		checks[check] = struct{}{}
	}
	calibration, okay := trustedSet(locks.CalibrationSHAs, 0)
	if !okay {
		return result, ErrUnqualified
	}
	fixtures, okay := trustedSet(locks.TestFixtureSHAs, 1)
	if !okay {
		return result, ErrUnqualified
	}
	evidence, okay := trustedSet(locks.EvidenceSHAs, 1)
	if !okay {
		return result, ErrUnqualified
	}
	for item := range fixtures {
		if _, overlap := calibration[item]; overlap {
			return result, ErrUnqualified
		}
	}
	return Expected{Operation: locks.Operation, Version: locks.Version,
		Backend: locks.Backend, ModelLockSHA: locks.ModelLockSHA,
		RuntimeLockSHA:   locks.RuntimeLockSHA,
		DeviceProfileSHA: locks.DeviceProfileSHA, DatasetSHA: locks.DatasetSHA,
		IssuerID: issuer, RequiredChecks: locks.RequiredChecks,
		CalibrationSHAs: calibration, TestFixtureSHAs: fixtures,
		EvidenceSHAs: evidence, LicenseCleared: true}, nil
}

// LoadLocks refuses mutable or symlinked paths to avoid a report changing its
// expected policy between operator inspection and execution.
func LoadLocks(path string) (Expected, error) {
	var empty Expected
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Mode().Perm()&022 != 0 ||
		before.Size() < 2 || before.Size() > 1024*1024 {
		return empty, ErrUnqualified
	}
	file, err := os.Open(path)
	if err != nil {
		return empty, ErrUnqualified
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) {
		return empty, ErrUnqualified
	}
	raw, err := io.ReadAll(io.LimitReader(file, 1024*1024+1))
	if err != nil || len(raw) < 2 || len(raw) > 1024*1024 {
		return empty, ErrUnqualified
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var locks DeploymentLocks
	if decoder.Decode(&locks) != nil || decoder.Decode(new(any)) != io.EOF {
		return empty, ErrUnqualified
	}
	return locks.expected()
}
