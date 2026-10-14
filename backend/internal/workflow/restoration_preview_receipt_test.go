package workflow

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestRestorationPreviewReceiptRequiresAllFourTasks(t *testing.T) {
	plan := restorationPreviewFixturePlan(t)
	value := RestorationPreviewReceipt{Run: plan.run, SourceResult: plan.source.ResultID,
		Before1600: plan.docs[0].ID, Before2560: plan.docs[1].ID,
		After1600: plan.docs[2].ID, After2560: plan.docs[3].ID, State: "running"}
	if err := validateRestorationPreviewReceipt(plan, value); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*RestorationPreviewReceipt){
		func(v *RestorationPreviewReceipt) { v.Run = uuid.New() },
		func(v *RestorationPreviewReceipt) { v.SourceResult = uuid.New() },
		func(v *RestorationPreviewReceipt) { v.Before1600 = v.After1600 },
		func(v *RestorationPreviewReceipt) { v.After2560 = uuid.Nil },
		func(v *RestorationPreviewReceipt) { v.State = "draft" },
	} {
		bad := value
		mutate(&bad)
		if validateRestorationPreviewReceipt(plan, bad) == nil {
			t.Fatal("invalid receipt accepted")
		}
	}
	if _, _, err := (RestorationPreviewIntentStore{}).ReadReceipt(context.Background(), plan); err == nil {
		t.Fatal("uninstalled receipt reader accepted")
	}
}
