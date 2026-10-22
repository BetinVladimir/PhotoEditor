package v2input

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2qualification"
	"sort"
)

// Admission resolves and verifies this pin from a deployment-selected policy
// and exact report bytes. These fields must never be copied from API input.
type SemanticQualificationPin struct {
	PolicyID         uuid.UUID `json:"policy_id"`
	QualificationID  uuid.UUID `json:"qualification_id"`
	PolicySHA        string    `json:"policy_sha"`
	Report           Ref       `json:"report"`
	Calibration      Ref       `json:"calibration"`
	ModelLockSHA     string    `json:"model_lock_sha"`
	RuntimeLockSHA   string    `json:"runtime_lock_sha"`
	DeviceProfileSHA string    `json:"device_profile_sha"`
	DatasetSHA       string    `json:"dataset_sha"`
	IssuerID         uuid.UUID `json:"issuer_id"`
	Backend          string    `json:"backend"`
}

func validSemanticQualificationPin(p SemanticQualificationPin) bool {
	return p.PolicyID != uuid.Nil && p.QualificationID != uuid.Nil && p.IssuerID != uuid.Nil && (p.Backend == "cuda" || p.Backend == "mps") && validRef(p.Report) && validRef(p.Calibration) && p.Report.ArtifactID != p.Calibration.ArtifactID && digestPattern.MatchString(p.PolicySHA) && digestPattern.MatchString(p.ModelLockSHA) && digestPattern.MatchString(p.RuntimeLockSHA) && digestPattern.MatchString(p.DeviceProfileSHA) && digestPattern.MatchString(p.DatasetSHA)
}

// CompileQualifiedSemanticSnapshotConfig extends full snapshot compilation
// with a value-owned qualification identity and binds every transfer's params.
// Live scope/report verification precedes this pure compiler at admission.
func CompileQualifiedSemanticSnapshotConfig(project, session, run uuid.UUID, snapshot Ref, body json.RawMessage, tasks []FrozenSemanticTask, pin SemanticQualificationPin) (SemanticDraftConfig, json.RawMessage, string, error) {
	if !validSemanticQualificationPin(pin) {
		return SemanticDraftConfig{}, nil, "", ErrInvalid
	}
	config, _, _, err := CompileSemanticSnapshotConfig(project, session, run, snapshot, body, tasks)
	if err != nil {
		return SemanticDraftConfig{}, nil, "", err
	}
	for _, task := range tasks {
		var profile struct {
			Backend string `json:"backend"`
		}
		if json.Unmarshal(task.profile, &profile) != nil || profile.Backend != pin.Backend {
			return SemanticDraftConfig{}, nil, "", ErrInvalid
		}
		params, err := DecodeSemanticTransferParams(task.closure.ParamsJCS)
		if err != nil || params.ModelLockSHA != pin.ModelLockSHA || params.Calibration != pin.Calibration {
			return SemanticDraftConfig{}, nil, "", ErrInvalid
		}
	}
	owned := pin
	config.Semantic.Qualification = &owned
	raw, err := canonical.Marshal(config)
	if err != nil || len(raw) > 8<<20 {
		return SemanticDraftConfig{}, nil, "", ErrInvalid
	}
	sha, err := canonical.Hash(config)
	if err != nil {
		return SemanticDraftConfig{}, nil, "", err
	}
	return config, raw, sha, nil
}

// SemanticQualificationPolicyHash hashes the full qualification acceptance
// policy, with set fields sorted. Changes to checks/issuer/license invalidate it.
func SemanticQualificationPolicyHash(policy uuid.UUID, e v2qualification.Expected) (string, error) {
	if policy == uuid.Nil || e.Operation != "semantic.transfer" || e.Version != "1" || (e.Backend != "cuda" && e.Backend != "mps") || e.IssuerID == uuid.Nil || !e.LicenseCleared || !digestPattern.MatchString(e.ModelLockSHA) || !digestPattern.MatchString(e.RuntimeLockSHA) || !digestPattern.MatchString(e.DeviceProfileSHA) || !digestPattern.MatchString(e.DatasetSHA) || len(e.RequiredChecks) < 1 || len(e.RequiredChecks) > 1000 {
		return "", ErrInvalid
	}
	checks := append([]string(nil), e.RequiredChecks...)
	sort.Strings(checks)
	for i, c := range checks {
		if len(c) < 1 || len(c) > 100 || i > 0 && checks[i-1] == c {
			return "", ErrInvalid
		}
	}
	values := func(set map[string]struct{}) ([]string, error) {
		if len(set) < 1 || len(set) > 10000 {
			return nil, ErrInvalid
		}
		out := make([]string, 0, len(set))
		for value := range set {
			if !digestPattern.MatchString(value) {
				return nil, ErrInvalid
			}
			out = append(out, value)
		}
		sort.Strings(out)
		return out, nil
	}
	fixtures, err := values(e.TestFixtureSHAs)
	if err != nil {
		return "", err
	}
	calibration, err := values(e.CalibrationSHAs)
	if err != nil {
		return "", err
	}
	evidence, err := values(e.EvidenceSHAs)
	if err != nil {
		return "", err
	}
	return canonical.Hash(map[string]any{"schema_version": 1, "policy_id": policy, "operation": e.Operation, "operation_version": e.Version, "backend": e.Backend, "model_lock_sha": e.ModelLockSHA, "runtime_lock_sha": e.RuntimeLockSHA, "device_profile_sha": e.DeviceProfileSHA, "dataset_sha": e.DatasetSHA, "issuer_id": e.IssuerID, "required_checks": checks, "test_fixture_shas": fixtures, "calibration_shas": calibration, "evidence_shas": evidence, "license_cleared": e.LicenseCleared})
}
