package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"testing"
)

type multiQualityProducerFixture struct {
	values  []*MultiQualityProducer
	cursors []uuid.UUID
}

func (s *multiQualityProducerFixture) Next(_ context.Context, after uuid.UUID) (*MultiQualityProducer, error) {
	s.cursors = append(s.cursors, after)
	if len(s.values) == 0 {
		return nil, nil
	}
	value := s.values[0]
	s.values = s.values[1:]
	return value, nil
}

type multiQualityPreparerFixture struct {
	fail           bool
	state          string
	tenants, tasks []uuid.UUID
	bounded        bool
	operations     []string
}

func (p *multiQualityPreparerFixture) Prepare(ctx context.Context, tenant, task uuid.UUID, operation string) (MultiQualityReceipt, error) {
	p.tenants = append(p.tenants, tenant)
	p.tasks = append(p.tasks, task)
	p.operations = append(p.operations, operation)
	_, p.bounded = ctx.Deadline()
	if p.fail {
		return MultiQualityReceipt{}, errors.New("temporary publication failure")
	}
	run := uuid.New()
	return MultiQualityReceipt{Run: run, SourceResult: uuid.New(), State: p.state,
		Quality: uuid.NewSHA1(run, []byte("quality.check:master"))}, nil
}
func TestMultiQualityProcessorAdvancesFailureAndWraps(t *testing.T) {
	a, b := uuid.MustParse("00000000-0000-0000-0000-000000000001"), uuid.MustParse("00000000-0000-0000-0000-000000000002")
	tenant := uuid.New()
	source := &multiQualityProducerFixture{values: []*MultiQualityProducer{{Tenant: tenant, Task: a, Operation: "optical.panorama"}, {Tenant: tenant, Task: b, Operation: "optical.focus_stack"}, nil, {Tenant: tenant, Task: a, Operation: "optical.panorama"}}}
	prepare := &multiQualityPreparerFixture{fail: true, state: "running"}
	processor := &MultiQualityProcessor{Source: source, Preparer: prepare}
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
func TestMultiQualityProcessorTerminalRaceAndInvalidState(t *testing.T) {
	for _, state := range []string{"running", "completed", "completed_with_errors", "failed", "cancelled", "draft"} {
		t.Run(state, func(t *testing.T) {
			source := &multiQualityProducerFixture{values: []*MultiQualityProducer{{Tenant: uuid.New(), Task: uuid.New(), Operation: "optical.panorama"}}}
			prepare := &multiQualityPreparerFixture{state: state}
			p := &MultiQualityProcessor{Source: source, Preparer: prepare}
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
func TestMultiQualityProcessorRefusesInvalidCandidateBeforeIO(t *testing.T) {
	source := &multiQualityProducerFixture{values: []*MultiQualityProducer{{Tenant: uuid.Nil, Task: uuid.New(), Operation: "optical.panorama"}}}
	prepare := &multiQualityPreparerFixture{state: "running"}
	p := &MultiQualityProcessor{Source: source, Preparer: prepare}
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
func TestMultiQualitySQLDiscoveryUnavailable(t *testing.T) {
	if _, err := (SQLMultiQualityProducers{}).Next(context.Background(), uuid.Nil); err == nil {
		t.Fatal("missing DB accepted")
	}
}

func TestMultiQualityProcessorRejectsForeignOperation(t *testing.T) {
	source := &multiQualityProducerFixture{values: []*MultiQualityProducer{{Tenant: uuid.New(), Task: uuid.New(), Operation: "media.preview"}}}
	prepare := &multiQualityPreparerFixture{state: "running"}
	processor := &MultiQualityProcessor{Source: source, Preparer: prepare}
	if ok, err := processor.ProcessOne(context.Background()); ok || err == nil || len(prepare.tasks) != 0 {
		t.Fatal("foreign operation reached preparation")
	}
}
