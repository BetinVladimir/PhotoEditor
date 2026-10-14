package semanticproposal

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"reflect"
)

// Owns native reference evidence independently of mutable input slices. Every
// target must use the same native reference; a mixed legacy/native batch refuses.
func bindNativeTransferPlanReference(request Request, session uuid.UUID, plan *WorkflowPlan, inputs []TransferTaskInputs, documents []v2input.SemanticPinnedDocuments) error {
	if plan == nil || len(inputs) == 0 {
		return v2input.ErrInvalid
	}
	var first *nativeSourceContinuationProof
	for _, input := range inputs {
		if input.nativeReference != nil {
			first = input.nativeReference
			break
		}
	}
	if first == nil {
		if plan.nativeReferenceRequired || plan.nativeReference != nil {
			return v2input.ErrInvalid
		}
		return nil
	}
	for _, input := range inputs {
		if input.nativeReference == nil {
			return v2input.ErrInvalid
		}
	}
	if len(documents) != len(inputs) || len(plan.sources) != 2*len(inputs) || plan.transferQualification == nil {
		return v2input.ErrInvalid
	}
	owned, err := composeNativeSourceContinuation(first.source, first.native, TransferColorProfile{sha: first.source.iccSHA})
	if err != nil {
		return err
	}
	if owned.source.tenant != request.TenantID || owned.source.project != plan.project || owned.source.actor != request.ActorID || owned.source.session != session || owned.source.binding != request.Intent.Reference || owned.native.protection.selection.root != request.Intent.ReferenceMask {
		return v2input.ErrInvalid
	}
	selected, err := selectQualificationPolicies(owned.native.defect.policy, []string{"semantic.transfer"})
	if err != nil {
		return err
	}
	var expected v2qualification.Expected
	if json.Unmarshal([]byte(plan.transferQualification.expected), &expected) != nil || !sameQualificationPolicyExpected(expected, selected["semantic.transfer"]) {
		return v2qualification.ErrUnqualified
	}
	registered := owned.native.protection.selection.registered
	for i, input := range inputs {
		if input.nativeReference == nil || !reflect.DeepEqual(*input.nativeReference, *first) || input.referenceSource == nil || *input.referenceSource != owned.source || plan.sources[2*i] != owned.source || input.sourcePerson != nil || input.targetPerson == nil || input.Dependencies.SourceProtection != registered.objects[0].ref {
			return v2input.ErrInvalid
		}
		pixels, err := v2input.ResolveSemanticMaskPixels(input.Dependencies.SourceProtection, documents[i].SourceProtection, request.Intent.Reference, documents[i].Reference, true)
		if err != nil || pixels != registered.objects[1].ref {
			return v2input.ErrInvalid
		}
	}
	plan.nativeReferenceRequired = true
	plan.nativeReference = &owned
	return nil
}
