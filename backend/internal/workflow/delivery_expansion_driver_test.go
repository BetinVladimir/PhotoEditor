package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/delivery"
	"reflect"
	"testing"
)

type deliveryDriverStore struct {
	calls          *[]string
	lease          *delivery.ExpansionLease
	plan           delivery.PreparePlan
	fail           string
	releasedCursor int
}

func (s *deliveryDriverStore) step(n string) error {
	*s.calls = append(*s.calls, n)
	if s.fail == n {
		return errors.New(n)
	}
	return nil
}
func (s *deliveryDriverStore) Claim(context.Context) (*delivery.ExpansionLease, error) {
	if e := s.step("claim"); e != nil {
		return nil, e
	}
	if s.lease == nil {
		return nil, nil
	}
	copy := *s.lease
	return &copy, nil
}
func (s *deliveryDriverStore) LoadPlan(context.Context, delivery.ExpansionLease) (delivery.PreparePlan, error) {
	return s.plan, s.step("plan")
}
func (s *deliveryDriverStore) Renew(context.Context, delivery.ExpansionLease) error {
	return s.step("renew")
}
func (s *deliveryDriverStore) Release(_ context.Context, l delivery.ExpansionLease) error {
	s.releasedCursor = l.Cursor
	return s.step("release")
}

type deliveryDriverChild struct {
	calls         *[]string
	fail, foreign bool
}

func (p deliveryDriverChild) ProcessChild(_ context.Context, l delivery.ExpansionLease, _ delivery.PreparePlan, c delivery.ExportChildIntent) (DeliveryChildAdmission, error) {
	*p.calls = append(*p.calls, "child")
	if p.fail {
		return DeliveryChildAdmission{}, errors.New("child")
	}
	if l.Cursor != c.SlotIndex {
		panic("cursor advanced before admission")
	}
	result := DeliveryChildAdmission{RunID: c.RunID, ExportID: uuid.New(), NextCursor: c.SlotIndex + 1}
	if p.foreign {
		result.NextCursor++
	}
	return result, nil
}
func TestDeliveryExpansionDriverFencesCursorAndReleases(t *testing.T) {
	plan, parent, _, _, _ := deliveryExportPlanFixture(t, nil)
	for _, scenario := range []string{"success", "empty", "claim", "plan", "renew", "child", "foreign", "release"} {
		t.Run(scenario, func(t *testing.T) {
			calls := []string{}
			store := &deliveryDriverStore{calls: &calls, plan: plan, fail: scenario, lease: &delivery.ExpansionLease{Run: parent, ParentTask: plan.TaskID(), SlotCount: 1}}
			if scenario == "empty" {
				store.lease = nil
			}
			processor := deliveryDriverChild{calls: &calls, fail: scenario == "child", foreign: scenario == "foreign"}
			result, err := (DeliveryExpansionDriver{Store: store, Children: processor,
				Assembly: &deliveryDriverAssembly{state: "pending"}, PageSize: 1}).ProcessOne(context.Background())
			expected := []string{"claim", "plan", "renew", "child", "release"}
			switch scenario {
			case "success", "release":
				expected = []string{"claim", "plan", "renew", "child", "renew", "release"}
			case "empty":
				expected = []string{"claim"}
				if err != nil || result.Claimed {
					t.Fatal(result, err)
				}
			case "claim":
				expected = []string{"claim"}
			case "plan":
				expected = []string{"claim", "plan", "release"}
			case "renew":
				expected = []string{"claim", "plan", "renew", "release"}
			}
			if !reflect.DeepEqual(calls, expected) {
				t.Fatal(calls, expected)
			}
			if scenario == "success" || scenario == "release" {
				if result.Admitted != 1 || result.Cursor != 1 || store.releasedCursor != 1 {
					t.Fatal(result)
				}
			} else if result.Admitted != 0 || result.Cursor != 0 {
				t.Fatal("unconfirmed cursor credited", result)
			}
			if scenario == "success" {
				if err != nil {
					t.Fatal(err)
				}
			} else if scenario != "empty" && err == nil {
				t.Fatal("failure accepted")
			}
		})
	}
}
func TestDeliveryExpansionDriverRejectsUnboundedPage(t *testing.T) {
	for _, size := range []int{0, -1, 101} {
		calls := []string{}
		_, err := (DeliveryExpansionDriver{Store: &deliveryDriverStore{calls: &calls}, Children: deliveryDriverChild{calls: &calls}, PageSize: size}).ProcessOne(context.Background())
		if err == nil || len(calls) != 0 {
			t.Fatal("invalid page reached SQL")
		}
	}
}

// Observation must preserve the admission cursor even when it advances a child.
type deliveryDriverObserver struct {
	slots   []int
	calls   int
	changed bool
	fail    bool
}

func (o *deliveryDriverObserver) PendingSlots(context.Context, delivery.ExpansionLease, int) ([]int, error) {
	return o.slots, nil
}
func (o *deliveryDriverObserver) ObserveChild(_ context.Context, l delivery.ExpansionLease, _ delivery.PreparePlan, c delivery.ExportChildIntent) (bool, error) {
	o.calls++
	if l.Cursor != 1 || c.SlotIndex != 0 {
		panic("observation changed cursor or child")
	}
	if o.fail {
		return false, errors.New("observe")
	}
	return o.changed, nil
}
func TestDeliveryExpansionDriverObservationPreservesCursor(t *testing.T) {
	plan, parent, _, _, _ := deliveryExportPlanFixture(t, nil)
	for _, scenario := range []string{"changed", "waiting", "failure", "foreign", "duplicate", "oversize"} {
		t.Run(scenario, func(t *testing.T) {
			calls := []string{}
			store := &deliveryDriverStore{calls: &calls, plan: plan, lease: &delivery.ExpansionLease{Run: parent, ParentTask: plan.TaskID(), SlotCount: 1, Cursor: 1}}
			observer := &deliveryDriverObserver{slots: []int{0}, changed: scenario == "changed", fail: scenario == "failure"}
			switch scenario {
			case "foreign":
				observer.slots = []int{1}
			case "duplicate":
				observer.slots = []int{0, 0}
			case "oversize":
				observer.slots = []int{0, 1}
			}
			result, err := (DeliveryExpansionDriver{Store: store, Children: deliveryDriverChild{calls: &calls}, Observer: observer,
				Assembly: &deliveryDriverAssembly{state: "pending"}, PageSize: 1}).ProcessOne(context.Background())
			if result.Cursor != 1 || result.Admitted != 0 || store.releasedCursor != 1 {
				t.Fatal("observation credited admission", result)
			}
			if scenario == "changed" {
				if err != nil || result.Observed != 1 || observer.calls != 1 {
					t.Fatal(result, err)
				}
			} else if result.Observed != 0 {
				t.Fatal(result)
			}
			if scenario == "waiting" && (err != nil || observer.calls != 1) {
				t.Fatal(result, err)
			}
			if scenario != "changed" && scenario != "waiting" && err == nil {
				t.Fatal("invalid observation accepted")
			}
			if (scenario == "foreign" || scenario == "duplicate" || scenario == "oversize") && observer.calls != 0 {
				t.Fatal("invalid page reached child")
			}
		})
	}
}

type deliveryDriverAssembly struct {
	calls int
	state string
	err   error
}

type deliveryDriverSigner struct {
	calls int
	state string
	err error
}

func (s *deliveryDriverSigner) ProcessSigning(_ context.Context, l delivery.ExpansionLease, _ delivery.PreparePlan) (string, error) {
	s.calls++
	if l.Cursor != l.SlotCount { panic("signing before complete admission") }
	return s.state, s.err
}

func TestDeliveryExpansionDriverSkipsSignerForOffPolicy(t *testing.T) {
	plan, parent, _, _, _ := deliveryExportPlanFixture(t, nil)
	for _, state := range []string{"pending", "failed", "adopted", "invalid"} {
		t.Run(state, func(t *testing.T) {
			calls := []string{}
			store := &deliveryDriverStore{calls: &calls, plan: plan,
				lease: &delivery.ExpansionLease{Run: parent, ParentTask: plan.TaskID(),
					SlotCount: 1, Cursor: 1}}
			signer := &deliveryDriverSigner{state: state}
			assembly := &deliveryDriverAssembly{state: "activated"}
			result, err := (DeliveryExpansionDriver{Store: store,
				Children: deliveryDriverChild{calls: &calls}, Signing: signer,
				Assembly: assembly, PageSize: 1}).ProcessOne(context.Background())
			if signer.calls != 0 || assembly.calls != 1 {
				t.Fatal("off policy invoked signer or skipped ordinary assembly", signer.calls, assembly.calls)
			}
			if err != nil || result.SigningState != "" {
				t.Fatal(result, err)
			}
		})
	}
}

func TestDeliveryExpansionDriverRequiresAssemblyAtCompleteCursor(t *testing.T) {
	plan, parent, _, _, _ := deliveryExportPlanFixture(t, nil)
	calls := []string{}
	store := &deliveryDriverStore{calls: &calls, plan: plan,
		lease: &delivery.ExpansionLease{Run: parent, ParentTask: plan.TaskID(),
			SlotCount: 1, Cursor: 1}}
	_, err := (DeliveryExpansionDriver{Store: store,
		Children: deliveryDriverChild{calls: &calls}, PageSize: 1}).ProcessOne(context.Background())
	if err == nil { t.Fatal("complete Delivery cursor silently skipped assembly") }
}

func boolToInt(value bool) int {
	if value { return 1 }
	return 0
}

func (a *deliveryDriverAssembly) ProcessAssembly(_ context.Context, l delivery.ExpansionLease, _ delivery.PreparePlan) (string, error) {
	a.calls++
	if l.Cursor != l.SlotCount {
		panic("assembly before complete admission")
	}
	return a.state, a.err
}
func TestDeliveryExpansionDriverAssemblyTransition(t *testing.T) {
	plan, parent, _, _, _ := deliveryExportPlanFixture(t, nil)
	for _, state := range []string{"pending", "failed", "finalized_failed", "activated", "invalid", "error"} {
		t.Run(state, func(t *testing.T) {
			calls := []string{}
			store := &deliveryDriverStore{calls: &calls, plan: plan, lease: &delivery.ExpansionLease{Run: parent, ParentTask: plan.TaskID(), SlotCount: 1, Cursor: 1}}
			assembly := &deliveryDriverAssembly{state: state}
			if state == "error" {
				assembly.err = errors.New("assembly")
			}
			result, err := (DeliveryExpansionDriver{Store: store, Children: deliveryDriverChild{calls: &calls}, Assembly: assembly, PageSize: 1}).ProcessOne(context.Background())
			if assembly.calls != 1 || result.Admitted != 0 || result.Cursor != 1 {
				t.Fatal(result, assembly.calls)
			}
			invalid := state == "invalid" || state == "error"
			if (err != nil) != invalid {
				t.Fatal("wrong assembly outcome", err)
			}
			released := false
			for _, call := range calls {
				if call == "release" {
					released = true
				}
			}
			if released == (state == "activated" || state == "finalized_failed") {
				t.Fatal("closed expansion released or open lease leaked", calls)
			}
			if !invalid && result.AssemblyState != state {
				t.Fatal("assembly state lost", result)
			}
		})
	}
}
