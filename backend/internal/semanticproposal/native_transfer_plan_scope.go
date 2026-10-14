package semanticproposal

import (
	"encoding/json"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// All native plan gates repeat source/reference identity and complete target
// cardinality before SQL. Only reference ordinals use native mask provenance.
func validateNativeTransferPlanScope(who auth.Identity, request Request, plan WorkflowPlan) error {
	if plan.nativeTargetsRequired {
		return v2input.ErrInvalid
	}
	if plan.nativeReferenceRequired != (plan.nativeReference != nil) {
		return v2input.ErrInvalid
	}
	if !plan.nativeReferenceRequired {
		return nil
	}
	if !plan.sourcesRequired || !plan.transferQualificationRequired || plan.transferQualification == nil {
		return v2input.ErrInvalid
	}
	n := len(plan.tasks)
	if n < 1 || n > 5000 || len(plan.inventory.Targets) != n || len(plan.sources) != 2*n || len(plan.protection) != 2*n || len(plan.person) != n {
		return v2input.ErrInvalid
	}
	native := plan.nativeReference
	source := native.source
	if source.tenant != request.TenantID || source.project != plan.project || source.actor != who.UserID || source.actor != request.ActorID || source.session != who.SessionID || source.binding != request.Intent.Reference {
		return v2input.ErrInvalid
	}
	if err := matchNativeSourceScope(source, native.native, TransferColorProfile{sha: source.iccSHA}); err != nil {
		return err
	}
	if err := matchNativeReferenceContinuation(native.native); err != nil {
		return err
	}
	selected, err := selectQualificationPolicies(native.native.defect.policy, []string{"semantic.transfer"})
	if err != nil {
		return err
	}
	var transferExpected v2qualification.Expected
	if json.Unmarshal([]byte(plan.transferQualification.expected), &transferExpected) != nil || !sameQualificationPolicyExpected(transferExpected, selected["semantic.transfer"]) {
		return v2qualification.ErrUnqualified
	}
	reader := SQLTransferSourceReader{ProjectID: plan.project, Bucket: source.bucket}
	if _, err := reader.marshalNativeSelectionPins(who, request, native.native.protection.selection); err != nil {
		return err
	}
	registered := native.native.protection.selection.registered
	for i, target := range plan.inventory.Targets {
		reference, destination := plan.protection[2*i], plan.protection[2*i+1]
		person := plan.person[i]
		if plan.sources[2*i] != source || plan.sources[2*i+1].binding != target || reference.Binding != request.Intent.Reference || reference.Root != registered.objects[0].ref || reference.Child != registered.objects[1].ref || destination.Binding != target || person.Binding != target || person.Protection != destination.Root || person.Expected != native.native.protection.person.expected || person.Qualification != native.native.protection.person.qualification || person.Evidence == "" || person.Count < 0 {
			return v2input.ErrInvalid
		}
	}
	return nil
}
