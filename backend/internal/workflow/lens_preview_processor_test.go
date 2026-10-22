package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"strings"
	"testing"
)

type lensPreviewProducerFixture struct {
	values  []*LensPreviewProducer
	cursors []uuid.UUID
}

func (s *lensPreviewProducerFixture) Next(_ context.Context, after uuid.UUID, _ string) (*LensPreviewProducer, error) {
	s.cursors = append(s.cursors, after)
	if len(s.values) == 0 {
		return nil, nil
	}
	value := s.values[0]
	s.values = s.values[1:]
	return value, nil
}

type lensPreviewPreparerFixture struct {
	fail           bool
	state          string
	tenants, tasks []uuid.UUID
	bounded        bool
}

func (p *lensPreviewPreparerFixture) Prepare(ctx context.Context, tenant, task uuid.UUID) (LensPreviewReceipt, error) {
	p.tenants = append(p.tenants, tenant)
	p.tasks = append(p.tasks, task)
	_, p.bounded = ctx.Deadline()
	if p.fail {
		return LensPreviewReceipt{}, errors.New("temporary publication failure")
	}
	run := uuid.New()
	return LensPreviewReceipt{Run: run, SourceResult: uuid.New(), State: p.state,
		Preview1600: uuid.NewSHA1(run, []byte("render.derivative:preview1600")),
		Preview2560: uuid.NewSHA1(run, []byte("render.derivative:preview2560"))}, nil
}
func TestLensPreviewProcessorAdvancesFailureAndWraps(t *testing.T) {
	a, b := uuid.MustParse("00000000-0000-0000-0000-000000000001"), uuid.MustParse("00000000-0000-0000-0000-000000000002")
	tenant := uuid.New()
	source := &lensPreviewProducerFixture{values: []*LensPreviewProducer{{Tenant: tenant, Task: a}, {Tenant: tenant, Task: b}, nil, {Tenant: tenant, Task: a}}}
	prepare := &lensPreviewPreparerFixture{fail: true, state: "running"}
	processor := &LensPreviewProcessor{Source: source, Preparer: prepare, SRGBSHA: strings.Repeat("a", 64)}
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
func TestLensPreviewProcessorTerminalRaceAndInvalidState(t *testing.T) {
	for _, state := range []string{"running", "completed", "completed_with_errors", "failed", "cancelled", "draft"} {
		t.Run(state, func(t *testing.T) {
			source := &lensPreviewProducerFixture{values: []*LensPreviewProducer{{Tenant: uuid.New(), Task: uuid.New()}}}
			prepare := &lensPreviewPreparerFixture{state: state}
			p := &LensPreviewProcessor{Source: source, Preparer: prepare, SRGBSHA: strings.Repeat("a", 64)}
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
func TestLensPreviewProcessorRefusesInvalidCandidateBeforeIO(t *testing.T) {
	source := &lensPreviewProducerFixture{values: []*LensPreviewProducer{{Tenant: uuid.Nil, Task: uuid.New()}}}
	prepare := &lensPreviewPreparerFixture{state: "running"}
	p := &LensPreviewProcessor{Source: source, Preparer: prepare, SRGBSHA: strings.Repeat("a", 64)}
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
func TestLensPreviewSQLDiscoveryUnavailable(t *testing.T) {
	if _, err := (SQLLensPreviewProducers{}).Next(context.Background(), uuid.Nil, strings.Repeat("a", 64)); err == nil {
		t.Fatal("missing DB accepted")
	}
}
