package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"strings"
	"testing"
)

type multiPreviewProducerFixture struct {
	values  []*MultiPreviewProducer
	cursors []uuid.UUID
}

func (s *multiPreviewProducerFixture) Next(_ context.Context, after uuid.UUID, _ string) (*MultiPreviewProducer, error) {
	s.cursors = append(s.cursors, after)
	if len(s.values) == 0 {
		return nil, nil
	}
	value := s.values[0]
	s.values = s.values[1:]
	return value, nil
}

type multiPreviewPreparerFixture struct {
	fail           bool
	state          string
	tenants, tasks []uuid.UUID
	bounded        bool
	operations     []string
}

func (p *multiPreviewPreparerFixture) Prepare(ctx context.Context, tenant, task uuid.UUID, operation string) (MultiPreviewReceipt, error) {
	p.tenants = append(p.tenants, tenant)
	p.tasks = append(p.tasks, task)
	p.operations = append(p.operations, operation)
	_, p.bounded = ctx.Deadline()
	if p.fail {
		return MultiPreviewReceipt{}, errors.New("temporary publication failure")
	}
	run := uuid.New()
	return MultiPreviewReceipt{Run: run, SourceResult: uuid.New(), State: p.state,
		Preview1600: uuid.NewSHA1(run, []byte("render.derivative:preview1600")),
		Preview2560: uuid.NewSHA1(run, []byte("render.derivative:preview2560"))}, nil
}
func TestMultiPreviewProcessorAdvancesFailureAndWraps(t *testing.T) {
	a, b := uuid.MustParse("00000000-0000-0000-0000-000000000001"), uuid.MustParse("00000000-0000-0000-0000-000000000002")
	tenant := uuid.New()
	source := &multiPreviewProducerFixture{values: []*MultiPreviewProducer{{Tenant: tenant, Task: a, Operation: "optical.panorama"}, {Tenant: tenant, Task: b, Operation: "optical.focus_stack"}, nil, {Tenant: tenant, Task: a, Operation: "optical.panorama"}}}
	prepare := &multiPreviewPreparerFixture{fail: true, state: "running"}
	processor := &MultiPreviewProcessor{Source: source, Preparer: prepare, SRGBSHA: strings.Repeat("a", 64)}
	if ok, err := processor.ProcessOne(context.Background()); ok || err == nil {
		t.Fatal("failed publication credited")
	}
	prepare.fail = false
	if ok, err := processor.ProcessOne(context.Background()); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if ok, err := processor.ProcessOne(context.Background()); ok || err != nil {
		t.Fatal(ok, err)
	}
	if ok, err := processor.ProcessOne(context.Background()); !ok || err != nil {
		t.Fatal(ok, err)
	}
	expected := []uuid.UUID{uuid.Nil, a, b, uuid.Nil}
	for i, cursor := range source.cursors {
		if cursor != expected[i] {
			t.Fatal("incorrect retry cursor", i)
		}
	}
	if !prepare.bounded || prepare.tasks[2] != a || prepare.tenants[0] != tenant || prepare.operations[1] != "optical.focus_stack" {
		t.Fatal("scope/deadline lost")
	}
}
func TestMultiPreviewProcessorTerminalRaceAndInvalidState(t *testing.T) {
	for _, state := range []string{"running", "completed", "completed_with_errors", "failed", "cancelled", "draft"} {
		t.Run(state, func(t *testing.T) {
			source := &multiPreviewProducerFixture{values: []*MultiPreviewProducer{{Tenant: uuid.New(), Task: uuid.New(), Operation: "optical.panorama"}}}
			prepare := &multiPreviewPreparerFixture{state: state}
			p := &MultiPreviewProcessor{Source: source, Preparer: prepare, SRGBSHA: strings.Repeat("a", 64)}
			ok, err := p.ProcessOne(context.Background())
			if state == "draft" {
				if ok || err == nil {
					t.Fatal("unsealed receipt accepted")
				}
			} else if !ok || err != nil {
				t.Fatal(ok, err)
			}
		})
	}
}
func TestMultiPreviewProcessorRefusesInvalidCandidateBeforeIO(t *testing.T) {
	source := &multiPreviewProducerFixture{values: []*MultiPreviewProducer{{Tenant: uuid.Nil, Task: uuid.New(), Operation: "optical.panorama"}}}
	prepare := &multiPreviewPreparerFixture{state: "running"}
	p := &MultiPreviewProcessor{Source: source, Preparer: prepare, SRGBSHA: strings.Repeat("a", 64)}
	if ok, err := p.ProcessOne(context.Background()); ok || err == nil {
		t.Fatal("invalid scope accepted")
	}
	if len(prepare.tasks) != 0 {
		t.Fatal("invalid scope reached IO")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.ProcessOne(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(source.cursors) != 1 {
		t.Fatal("cancelled call discovered source")
	}
}
func TestMultiPreviewSQLDiscoveryUnavailable(t *testing.T) {
	if _, err := (SQLMultiPreviewProducers{}).Next(context.Background(), uuid.Nil, strings.Repeat("a", 64)); err == nil {
		t.Fatal("missing DB accepted")
	}
}

func TestMultiPreviewProcessorRejectsForeignOperation(t *testing.T) {
	source := &multiPreviewProducerFixture{values: []*MultiPreviewProducer{{Tenant: uuid.New(), Task: uuid.New(), Operation: "media.preview"}}}
	prepare := &multiPreviewPreparerFixture{state: "running"}
	processor := &MultiPreviewProcessor{Source: source, Preparer: prepare, SRGBSHA: strings.Repeat("a", 64)}
	if ok, err := processor.ProcessOne(context.Background()); ok || err == nil || len(prepare.tasks) != 0 {
		t.Fatal("foreign operation reached preparation")
	}
}
