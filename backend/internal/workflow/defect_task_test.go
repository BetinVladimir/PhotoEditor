package workflow

import (
	"context"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
)

func taskFixture(t *testing.T) (DefectExpansionInput, MaterializedDefectBranch, []ArtifactSize) {
	t.Helper()
	input := expansionFixture(t)
	branch, err := MaterializeDefectBranch(context.Background(), new(documentPublisher), input)
	if err != nil {
		t.Fatal(err)
	}
	sizes := []ArtifactSize{}
	for _, ref := range append(append([]review.ArtifactRef(nil), branch.Inputs...), *branch.Params) {
		sizes = append(sizes, ArtifactSize{ref, 1048576})
	}
	return input, branch, sizes
}
func TestRepairTaskStableAndAdmitsFullFiles(t *testing.T) {
	in, b, s := taskFixture(t)
	plan, err := PrepareDefectTask(in.RunID, in.ReviewID, in.Protection.Canvas, b, s)
	if err != nil {
		t.Fatal(err)
	}
	estimate, err := ParseResourceEstimate(plan.ExecutionProfile)
	if err != nil {
		t.Fatal(err)
	}
	if estimate.RAMBytes < 1073741824 || estimate.VRAMBytes != 0 || estimate.OutputBytes < 12*estimate.PixelCount+2*1048576 || estimate.ScratchBytes < estimate.OutputBytes+int64(len(s))*1048576 {
		t.Fatal("underadmitted repair", estimate)
	}
	for i, j := 0, len(b.Inputs)-1; i < j; i, j = i+1, j-1 {
		b.Inputs[i], b.Inputs[j] = b.Inputs[j], b.Inputs[i]
	}
	second, err := PrepareDefectTask(in.RunID, in.ReviewID, in.Protection.Canvas, b, s)
	if err != nil || !reflect.DeepEqual(plan, second) {
		t.Fatal("input permutation changed plan", err)
	}
	other, err := PrepareDefectTask(uuid.New(), in.ReviewID, in.Protection.Canvas, b, s)
	if err != nil || other.ID == plan.ID {
		t.Fatal("task identity shared by different runs", err)
	}
}
func TestRepairTaskRejectsMissingStaleOrOverflowMetadata(t *testing.T) {
	for _, kind := range []string{"missing", "duplicate", "sha", "overflow", "params-size", "selection", "selection-is-params"} {
		t.Run(kind, func(t *testing.T) {
			in, b, s := taskFixture(t)
			switch kind {
			case "missing":
				s = s[1:]
			case "duplicate":
				s = append(s, s[0])
			case "sha":
				s[0].Ref.SHA256 = "bad"
			case "overflow":
				s[0].Bytes = 9007199254740991
			case "params-size":
				s[len(s)-1].Bytes = 1048577
			case "selection-is-params":
				b.Selection = b.Params
			case "selection":
				ref := *b.Selection
				ref.ID = uuid.New()
				b.Selection = &ref
			}
			if _, err := PrepareDefectTask(in.RunID, in.ReviewID, in.Protection.Canvas, b, s); err == nil {
				t.Fatal("unsafe task accepted")
			}
		})
	}
}
