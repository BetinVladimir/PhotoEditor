package workflow

import (
	"context"
	"photoeditor/backend/internal/delivery"
	"reflect"
	"testing"
)

func TestDeliveryRouterMergesBoundedOrderedCandidates(t *testing.T) {
	for _, tc := range []struct {
		a, b, want []int
		limit      int
		bad        bool
	}{
		{[]int{0, 2}, []int{1, 3}, []int{0, 1, 2, 3}, 4, false},
		{[]int{0, 2}, []int{1, 3}, []int{0, 1}, 2, false},
		{[]int{0, 2}, []int{1, 2}, nil, 3, true},
		{nil, nil, []int{}, 3, false},
		{[]int{2, 1}, nil, nil, 3, true},
		{nil, []int{1, 1}, nil, 3, true},
		{[]int{4}, nil, nil, 3, true},
		{[]int{0, 1, 2}, nil, nil, 2, true},
	} {
		got, err := mergeDeliveryPendingSlots(tc.a, tc.b, 4, tc.limit)
		if (err != nil) != tc.bad || !tc.bad && !reflect.DeepEqual(got, tc.want) {
			t.Fatal(tc, got, err)
		}
	}
}
func TestDeliveryRouterRoutesOrdinaryFrozenChild(t *testing.T) {
	plan, parent, child, _, _ := deliveryExportPlanFixture(t, nil)
	calls := []string{}
	target := deliveryDriverChild{calls: &calls}
	router := DeliveryChildRouter{Ordinary: target}
	lease := delivery.ExpansionLease{Run: parent, Cursor: child.SlotIndex}
	if _, err := router.ProcessChild(context.Background(), lease, plan, child); err != nil || !reflect.DeepEqual(calls, []string{"child"}) {
		t.Fatal(calls, err)
	}
	foreign := child
	foreign.SlotIndex++
	if _, err := router.ProcessChild(context.Background(), lease, plan, foreign); err == nil || len(calls) != 1 {
		t.Fatal("invalid selection routed", calls, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := router.ProcessChild(ctx, lease, plan, child); err != context.Canceled || len(calls) != 1 {
		t.Fatal(calls, err)
	}
}
