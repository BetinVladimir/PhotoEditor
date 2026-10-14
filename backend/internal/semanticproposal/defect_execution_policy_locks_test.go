package semanticproposal

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2qualification"
	"strings"
	"testing"
)

func TestDefectExecutionMatchesSingleOriginalPolicySnapshot(t *testing.T) {
	sha := strings.Repeat("a", 64)
	operations := []string{"defects.propose", "face.landmarks", "face.detect", "face.parse"}
	rows := make([]qualificationPolicyRow, 0, 4)
	for _, operation := range operations {
		rows = append(rows, qualificationPolicyRow{ID: uuid.New(), Operation: operation, Version: "1", Model: sha, Runtime: sha, Device: sha, Dataset: sha, Backend: "cpu", Issuer: uuid.New(), Checks: []string{"quality"}, Fixtures: []string{sha}, Evidence: []string{sha}, License: true, Enabled: true, Admission: true})
	}
	body, err := canonical.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	policy := qualificationPolicySnapshot{body: string(body)}
	selected, err := selectQualificationPolicies(policy, operations)
	if err != nil {
		t.Fatal(err)
	}
	defectBody, err := canonical.Marshal(selected["defects.propose"])
	if err != nil {
		t.Fatal(err)
	}
	expected := [4]v2qualification.Expected{selected["face.landmarks"], selected["face.detect"], selected["face.parse"], selected["face.detect"]}
	masksBody, err := canonical.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	prepared := defectMaskExecutionPreparation{face: defectFaceExecutionPreparation{source: defectSourceExecutionPreparation{expected: string(defectBody)}}, expected: string(masksBody)}
	if err := matchDefectExecutionPolicySnapshot(prepared, policy); err != nil {
		t.Fatal("original policies rejected", err)
	}
	changed := expected
	changed[3] = ownQualificationExpected(changed[3])
	changed[3].RuntimeLockSHA = strings.Repeat("b", 64)
	changedBody, err := canonical.Marshal(changed)
	if err != nil {
		t.Fatal(err)
	}
	bad := prepared
	bad.expected = string(changedBody)
	if err := matchDefectExecutionPolicySnapshot(bad, policy); err != v2qualification.ErrUnqualified {
		t.Fatal("mixed detector policy accepted", err)
	}
	rows[3].Admission = false
	body, err = canonical.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	if err := matchDefectExecutionPolicySnapshot(prepared, qualificationPolicySnapshot{body: string(body)}); err != v2qualification.ErrUnqualified {
		t.Fatal("missing parser admission accepted", err)
	}
}
