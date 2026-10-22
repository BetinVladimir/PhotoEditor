package semanticproposal

import "fmt"

type defectAdmissionStage string

const (
	defectStageRegisteredContext     defectAdmissionStage = "registered_context"
	defectStageRegisteredTransaction defectAdmissionStage = "registered_transaction"
	defectStageRegistrationPacket    defectAdmissionStage = "registration_packet"
	defectStageAdmissionPacket       defectAdmissionStage = "admission_packet"
	defectStagePersonAuthority       defectAdmissionStage = "person_authority"
	defectStageRegistrationQuery     defectAdmissionStage = "registration_query"
	defectStageRegistrationState     defectAdmissionStage = "registration_state"
	defectStageRegisteredPolicy      defectAdmissionStage = "registered_policy"
	defectStageEvidenceQuery         defectAdmissionStage = "evidence_query"
	defectStageEvidenceState         defectAdmissionStage = "evidence_state"
	defectStageRegisteredCompletion  defectAdmissionStage = "registered_completion"
	defectStagePersonTransaction     defectAdmissionStage = "person_transaction"
	defectStagePersonValidation      defectAdmissionStage = "person_validation"
	defectStageBeginDraft            defectAdmissionStage = "begin_draft"
	defectStagePersonProofs          defectAdmissionStage = "person_proofs"
	defectStagePersonPacket          defectAdmissionStage = "person_packet"
	defectStageActivationContent     defectAdmissionStage = "activation_content"
	defectStagePersonQuery           defectAdmissionStage = "person_query"
	defectStagePersonState           defectAdmissionStage = "person_state"
	defectStagePersonPolicy          defectAdmissionStage = "person_policy"
)

// Adds only a fixed internal stage; Unwrap preserves errors.Is/errors.As and public mapping.
func defectAdmissionStageError(stage defectAdmissionStage, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("defect admission %s: %w", stage, err)
}
