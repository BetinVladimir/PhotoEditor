package semanticproposal

import (
	"encoding/json"
	"photoeditor/backend/internal/v2qualification"
)

// Full native MPS preparation requires a consistent independently qualified
// backend across its model-bearing policy axes, preserving the existing CPU defect auxiliary. Generic policy selection remains
// unchanged for independent producer clients and existing CUDA paths.
func selectNativeQualificationPolicies(snapshot qualificationPolicySnapshot, required []string) (map[string]v2qualification.Expected, error) {
	selected, err := selectQualificationPolicies(snapshot, required)
	if err != nil {
		return nil, err
	}
	metal := false
	var rows []qualificationPolicyRow
	if json.Unmarshal([]byte(snapshot.body), &rows) != nil {
		return nil, v2qualification.ErrUnqualified
	}
	for _, row := range rows {
		if row.Admission && row.Version == "1" && row.Backend == "mps" {
			metal = true
		}
	}
	if !metal {
		return selected, nil
	}
	all, err := selectQualificationPolicies(snapshot, []string{"mask.person", "face.detect", "face.landmarks", "face.parse", "defects.propose", "semantic.transfer"})
	if err != nil {
		return nil, err
	}
	for op, e := range all {
		expected := "mps"
		if op == "defects.propose" {
			expected = "cpu"
		}
		if e.Backend != expected {
			return nil, v2qualification.ErrUnqualified
		}
	}
	return selected, nil
}
