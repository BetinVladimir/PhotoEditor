package semanticproposal

import (
	"github.com/google/uuid"
	"testing"
)

func TestDetectorStageIdentitySeparatesParentAndDefectStage(t *testing.T) {
	parent := uuid.New()
	run, workflow, err := detectorPreparationIDs(parent)
	if err != nil {
		t.Fatal(err)
	}
	againRun, againWorkflow, err := detectorPreparationIDs(parent)
	if err != nil || run != againRun || workflow != againWorkflow {
		t.Fatal("retry identity changed", err)
	}
	otherRun, otherWorkflow, err := detectorPreparationIDs(uuid.New())
	if err != nil || otherRun == run || otherWorkflow == workflow {
		t.Fatal("parents share detector stage", err)
	}
	defectRun, defectWorkflow, err := defectPreparationIDs(parent)
	if err != nil || run == defectRun || workflow == defectWorkflow {
		t.Fatal("detector aliases defect stage", err)
	}
	if _, _, err := detectorPreparationIDs(uuid.Nil); err == nil {
		t.Fatal("empty parent accepted")
	}
}
