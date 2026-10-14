package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"testing"
)

type qualityProducerFixture struct {
	values  []*PerspectiveQualityProducer
	cursors []uuid.UUID
}

func (s *qualityProducerFixture) Next(_ context.Context, after uuid.UUID) (*PerspectiveQualityProducer, error) {
	s.cursors = append(s.cursors, after)
	if len(s.values) == 0 {
		return nil, nil
	}
	value := s.values[0]
	s.values = s.values[1:]
	return value, nil
}

type qualityPreparerFixture struct {
	fail           bool
	state          string
	tenants, tasks []uuid.UUID
	bounded        bool
}

func (p *qualityPreparerFixture) Prepare(ctx context.Context, tenant, task uuid.UUID) (PerspectiveQualityReceipt, error) {
	p.tenants = append(p.tenants, tenant)
	p.tasks = append(p.tasks, task)
	_, p.bounded = ctx.Deadline()
	if p.fail {
		return PerspectiveQualityReceipt{}, errors.New("temporary publication failure")
	}
	run := uuid.New()
	return PerspectiveQualityReceipt{Run: run, SourceResult: uuid.New(), State: p.state,
		Quality: uuid.NewSHA1(run, []byte("quality.check:master"))}, nil
}
func TestPerspectiveQualityProcessorAdvancesFailureAndWraps(t *testing.T) {
	a, b := uuid.MustParse("00000000-0000-0000-0000-000000000001"), uuid.MustParse("00000000-0000-0000-0000-000000000002")
	tenant := uuid.New()
	source := &qualityProducerFixture{values: []*PerspectiveQualityProducer{{Tenant: tenant, Task: a}, {Tenant: tenant, Task: b}, nil, {Tenant: tenant, Task: a}}}
	prepare := &qualityPreparerFixture{fail: true, state: "running"}
	processor := &PerspectiveQualityProcessor{Source: source, Preparer: prepare}
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
	if !prepare.bounded || prepare.tasks[2] != a || prepare.tenants[0] != tenant {
		t.Fatal("scope/deadline lost")
	}
}
func TestPerspectiveQualityProcessorTerminalRaceAndInvalidState(t *testing.T) {
	for _, state := range []string{"running", "completed", "completed_with_errors", "failed", "cancelled", "draft"} {
		t.Run(state, func(t *testing.T) {
			source := &qualityProducerFixture{values: []*PerspectiveQualityProducer{{Tenant: uuid.New(), Task: uuid.New()}}}
			prepare := &qualityPreparerFixture{state: state}
			p := &PerspectiveQualityProcessor{Source: source, Preparer: prepare}
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
func TestPerspectiveQualityProcessorRefusesInvalidCandidateBeforeIO(t *testing.T) {
	source := &qualityProducerFixture{values: []*PerspectiveQualityProducer{{Tenant: uuid.Nil, Task: uuid.New()}}}
	prepare := &qualityPreparerFixture{state: "running"}
	p := &PerspectiveQualityProcessor{Source: source, Preparer: prepare}
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
func TestPerspectiveQualitySQLDiscoveryUnavailable(t *testing.T) {
	if _, err := (SQLPerspectiveQualityProducers{}).Next(context.Background(), uuid.Nil); err == nil {
		t.Fatal("missing DB accepted")
	}
}
