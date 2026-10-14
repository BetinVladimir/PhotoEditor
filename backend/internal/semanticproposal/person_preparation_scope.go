package semanticproposal

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Shape/scope check only; original content and scientific qualification must
// already have been verified. Canonical ownership does not grant SQL authority.
func validPersonPreparationScope(s SQLTransferSourceReader, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, proof personProtectionProof) bool {
	if proof.Binding != binding || proof.Protection != proof.protectionEvidence.root.ref || proof.Count < 0 || proof.Count > 200000000 || !validProtectionPreparationScope(s, who, tenant, binding, proof.protectionEvidence) {
		return false
	}
	seen := map[uuid.UUID]bool{binding.Source.ArtifactID: true, proof.Protection.ArtifactID: true, proof.protectionEvidence.pixels.ref.ArtifactID: true}
	for _, ref := range []v2input.Ref{proof.Root, proof.Child, proof.Report} {
		if ref.ArtifactID == uuid.Nil || seen[ref.ArtifactID] {
			return false
		}
		seen[ref.ArtifactID] = true
		if _, err := domain.ParseDigest(ref.SHA256); err != nil {
			return false
		}
	}
	if len(proof.Expected) > 4194304 || len(proof.Evidence) > 32768 || len(proof.Qualification) > 16384 {
		return false
	}
	var expected v2qualification.Expected
	if json.Unmarshal([]byte(proof.Expected), &expected) != nil || expected.Operation != "mask.person" || expected.Version != "1" || (expected.Backend != "cpu" && expected.Backend != "cuda" && expected.Backend != "mps") {
		return false
	}
	body, err := canonical.Marshal(expected)
	if err != nil || string(body) != proof.Expected {
		return false
	}
	var evidence []personEvidencePin
	var report personEvidencePin
	if json.Unmarshal([]byte(proof.Evidence), &evidence) != nil || len(evidence) != 2 || json.Unmarshal([]byte(proof.Qualification), &report) != nil {
		return false
	}
	body, err = canonical.Marshal(evidence)
	if err != nil || string(body) != proof.Evidence {
		return false
	}
	body, err = canonical.Marshal(report)
	if err != nil || string(body) != proof.Qualification {
		return false
	}
	if report.Contract != "QualificationReport" || report.ArtifactID != proof.Report.ArtifactID || report.SHA256 != proof.Report.SHA256 {
		return false
	}
	for i, pin := range append(evidence, report) {
		if i < 2 && (pin.Contract != [2]string{"PersonParams", "PersonReport"}[i] || pin.ArtifactID == uuid.Nil || seen[pin.ArtifactID]) {
			return false
		}
		if i < 2 {
			seen[pin.ArtifactID] = true
		}
		if pin.Bucket != s.Bucket || pin.Key == "" || pin.Version == "" || pin.Version == "null" || pin.Bytes < 2 || pin.Bytes > 8388608 || pin.Backend != expected.Backend {
			return false
		}
		if _, err := domain.ParseDigest(pin.SHA256); err != nil {
			return false
		}
	}
	return true
}
