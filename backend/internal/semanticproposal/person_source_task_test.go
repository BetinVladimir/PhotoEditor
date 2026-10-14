package semanticproposal

import (
	"github.com/google/uuid"
	"testing"
)

func TestPersonPreparationIdentityIsStableAndIndependent(t *testing.T) {
	parent := uuid.New()
	run, workflow, err := personPreparationIDs(parent)
	if err != nil {
		t.Fatal(err)
	}
	a, b, err := personPreparationIDs(parent)
	if err != nil || a != run || b != workflow {
		t.Fatal("retry changed person stage", err)
	}
	detector, detectorWorkflow, err := detectorPreparationIDs(parent)
	if err != nil || detector == run || detectorWorkflow == workflow || run == parent {
		t.Fatal("person aliases detector/parent", err)
	}
	other, _, err := personPreparationIDs(uuid.New())
	if err != nil || other == run {
		t.Fatal("different parents share person stage", err)
	}
	if _, _, err := personPreparationIDs(uuid.Nil); err == nil {
		t.Fatal("empty parent accepted")
	}
}
