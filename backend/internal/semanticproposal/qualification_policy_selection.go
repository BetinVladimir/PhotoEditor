package semanticproposal

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2qualification"
	"sort"
)

type qualificationPolicyRow struct {
	ID           uuid.UUID `json:"id"`
	Operation    string    `json:"operation"`
	Version      string    `json:"operation_version"`
	Model        string    `json:"model_lock_sha"`
	Runtime      string    `json:"runtime_lock_sha"`
	Backend      string    `json:"backend"`
	Device       string    `json:"device_profile_sha"`
	Dataset      string    `json:"dataset_sha"`
	Issuer       uuid.UUID `json:"issuer_id"`
	Checks       []string  `json:"required_checks"`
	Fixtures     []string  `json:"test_fixture_shas"`
	Calibrations []string  `json:"calibration_shas"`
	Evidence     []string  `json:"evidence_shas"`
	License      bool      `json:"license_cleared"`
	Enabled      bool      `json:"enabled"`
	Admission    bool      `json:"admission_enabled"`
}

// Selects the DBA admission policy for every server-required operation. No
// newest-model selection, fallback or scientific qualification is performed.
func selectQualificationPolicies(snapshot qualificationPolicySnapshot, required []string) (map[string]v2qualification.Expected, error) {
	fail := func() (map[string]v2qualification.Expected, error) { return nil, v2qualification.ErrUnqualified }
	if len(snapshot.body) > 4194304 || len(required) < 1 || len(required) > 6 {
		return fail()
	}
	allowed := map[string]bool{"mask.person": true, "face.detect": true, "face.landmarks": true, "face.parse": true, "defects.propose": true, "semantic.transfer": true}
	wanted := map[string]bool{}
	for _, op := range required {
		if !allowed[op] || wanted[op] {
			return fail()
		}
		wanted[op] = true
	}
	var rows []qualificationPolicyRow
	if json.Unmarshal([]byte(snapshot.body), &rows) != nil || len(rows) < 1 || len(rows) > 64 {
		return fail()
	}
	result := map[string]v2qualification.Expected{}
	seen := map[uuid.UUID]bool{}
	for _, row := range rows {
		if row.ID == uuid.Nil || seen[row.ID] || !allowed[row.Operation] {
			return fail()
		}
		seen[row.ID] = true
		if !row.Admission || row.Version != "1" {
			continue
		}
		if !row.Enabled || !row.License || row.Issuer == uuid.Nil || (row.Backend != "cpu" && row.Backend != "cuda" && row.Backend != "mps") {
			return fail()
		}
		for _, sha := range []string{row.Model, row.Runtime, row.Device, row.Dataset} {
			if _, err := domain.ParseDigest(sha); err != nil {
				return fail()
			}
		}
		if _, exists := result[row.Operation]; exists {
			return fail()
		}
		if len(row.Checks) == 0 || len(row.Checks) > 256 {
			return fail()
		}
		checks := append([]string(nil), row.Checks...)
		sort.Strings(checks)
		for i, check := range checks {
			if check == "" || len(check) > 256 || (i > 0 && check == checks[i-1]) {
				return fail()
			}
		}
		sets := [3]map[string]struct{}{}
		for i, values := range [][]string{row.Fixtures, row.Calibrations, row.Evidence} {
			if len(values) > 4096 || (i != 1 && len(values) == 0) {
				return fail()
			}
			sets[i] = map[string]struct{}{}
			for _, sha := range values {
				if _, err := domain.ParseDigest(sha); err != nil {
					return fail()
				}
				if _, exists := sets[i][sha]; exists {
					return fail()
				}
				sets[i][sha] = struct{}{}
			}
		}
		result[row.Operation] = v2qualification.Expected{Operation: row.Operation, Version: row.Version, Backend: row.Backend, ModelLockSHA: row.Model, RuntimeLockSHA: row.Runtime, DeviceProfileSHA: row.Device, DatasetSHA: row.Dataset, IssuerID: row.Issuer, RequiredChecks: checks, TestFixtureSHAs: sets[0], CalibrationSHAs: sets[1], EvidenceSHAs: sets[2], LicenseCleared: row.License}
	}
	for op := range wanted {
		if _, ok := result[op]; !ok {
			return fail()
		}
	}
	selected := map[string]v2qualification.Expected{}
	for op := range wanted {
		selected[op] = result[op]
	}
	return selected, nil
}
