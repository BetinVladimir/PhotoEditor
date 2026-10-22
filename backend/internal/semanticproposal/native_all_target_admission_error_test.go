package semanticproposal

import (
	"errors"
	"photoeditor/backend/internal/v2input"
	"testing"
)

type admissionDiagnosticCause struct{}

func (*admissionDiagnosticCause) Error() string { return "cause" }
func TestNativeAllTargetAdmissionErrorPreservesCause(t *testing.T) {
	for _, stage := range []string{"publish_params", "begin_draft", "register_inputs", "persist_parent", "foreign-secret"} {
		if nativeAllTargetAdmissionError(stage, nil) != nil {
			t.Fatal("nil changed")
		}
		if !errors.Is(nativeAllTargetAdmissionError(stage, v2input.ErrConflict), v2input.ErrConflict) {
			t.Fatal("conflict lost")
		}
		cause := &admissionDiagnosticCause{}
		var got *admissionDiagnosticCause
		if !errors.As(nativeAllTargetAdmissionError(stage, cause), &got) || got != cause {
			t.Fatal("typed cause lost")
		}
	}
	if nativeAllTargetAdmissionError("foreign-secret", v2input.ErrConflict).Error() != "native all-target admission unknown: "+v2input.ErrConflict.Error() {
		t.Fatal("unclosed label")
	}
}
