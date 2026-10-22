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

type cancellationDeliveryObserver struct{calls *int;cancel context.CancelFunc;slots []int}
func(o cancellationDeliveryObserver)PendingSlots(context.Context,delivery.ExpansionLease,int)([]int,error){(*o.calls)++;if o.cancel!=nil{o.cancel()};return o.slots,nil}
func(o cancellationDeliveryObserver)ObserveChild(context.Context,delivery.ExpansionLease,delivery.PreparePlan,delivery.ExportChildIntent)(bool,error){return false,nil}
func TestDeliveryPendingDiscoveryCancellationAfterEitherBranch(t *testing.T){
 for _,branch:=range []string{"ordinary","watermark"}{
  ctx,cancel:=context.WithCancel(context.Background())
  first,second:=0,0
  ordinary:=cancellationDeliveryObserver{calls:&first,slots:[]int{0}}
  watermark:=cancellationDeliveryObserver{calls:&second,slots:[]int{1}}
  if branch=="ordinary"{ordinary.cancel=cancel}else{watermark.cancel=cancel}
  result,err:=(DeliveryChildRouter{OrdinaryObserver:ordinary,WatermarkObserver:watermark}).PendingSlots(ctx,delivery.ExpansionLease{Cursor:2,SlotCount:2},2)
  cancel()
  if err!=context.Canceled||result!=nil||first!=1||branch=="ordinary"&&second!=0||branch=="watermark"&&second!=1{t.Fatal(branch,result,err,first,second)}
 }
}
