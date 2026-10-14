package semanticproposal

import (
	"encoding/json"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Pure native reference validation runs before durable publication and again
// before target preparation. Qualification report authority remains independent
// of caller-selected masks and is bound to the original person proof.
func (s SQLTransferSourceReader) validateNativeTransferReference(who auth.Identity, request Request, configuration TransferPreparation, native nativeTransferReferenceDocuments) (nativeSourceContinuationProof, error) {
	var zero nativeSourceContinuationProof
	if len(native.reference) == 0 || len(native.selected) == 0 || len(native.protection) == 0 {
		return zero, v2input.ErrInvalid
	}
	if configuration.PersonProtection == nil || native.proof.source.binding != request.Intent.Reference || native.proof.source.tenant != request.TenantID || native.proof.source.actor != who.UserID || native.proof.source.session != who.SessionID || native.proof.source.project != s.ProjectID || native.proof.source.bucket != s.Bucket || native.proof.native.protection.selection.registered.objects[0].ref != configuration.SourceProtection {
		return zero, v2input.ErrInvalid
	}
	owned, err := composeNativeSourceContinuation(native.proof.source, native.proof.native, configuration.Color)
	if err != nil {
		return zero, err
	}
	body, err := canonical.Marshal(configuration.PersonProtection.Expected)
	if err != nil || string(body) != owned.native.protection.person.expected {
		return zero, v2input.ErrConflict
	}
	var report personEvidencePin
	if json.Unmarshal([]byte(owned.native.protection.person.qualification), &report) != nil || report.ArtifactID != configuration.PersonProtection.Report.ArtifactID || report.SHA256 != configuration.PersonProtection.Report.SHA256 {
		return zero, v2input.ErrConflict
	}
	if owned.native.protection.selection.root != request.Intent.ReferenceMask {
		return zero, v2input.ErrConflict
	}
	return owned, nil
}
