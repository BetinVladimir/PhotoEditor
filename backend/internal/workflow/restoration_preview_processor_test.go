package workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

type restorationPreviewQueueFixture struct {
	producer RestorationPreviewProducer
	cursors  []uuid.UUID
}

func (q *restorationPreviewQueueFixture) Next(_ context.Context, after uuid.UUID,
	_ string) (*RestorationPreviewProducer, error) {
	q.cursors = append(q.cursors, after)
	if after != uuid.Nil {
		return nil, nil
	}
	return &q.producer, nil
}

type restorationPreviewPreparerFixture struct {
	calls   int
	fail    bool
	receipt RestorationPreviewReceipt
}

func (p *restorationPreviewPreparerFixture) Prepare(_ context.Context, _, _ uuid.UUID) (RestorationPreviewReceipt, error) {
	p.calls++
	if p.fail {
		return RestorationPreviewReceipt{}, errors.New("temporary source failure")
	}
	return p.receipt, nil
}

func TestRestorationPreviewRetriesFailedProducerAfterCursorWrap(t *testing.T) {
	producer := RestorationPreviewProducer{Tenant: uuid.New(), Task: uuid.New()}
	run := uuid.New()
	queue := &restorationPreviewQueueFixture{producer: producer}
	preparer := &restorationPreviewPreparerFixture{fail: true,
		receipt: RestorationPreviewReceipt{Run: run, SourceResult: uuid.New(),
			Before1600: restorationPreviewTaskID(run, "before", "preview1600"),
			Before2560: restorationPreviewTaskID(run, "before", "preview2560"),
			After1600:  restorationPreviewTaskID(run, "after", "preview1600"),
			After2560:  restorationPreviewTaskID(run, "after", "preview2560"),
			State:      "running"}}
	processor := &RestorationPreviewProcessor{Source: queue, Preparer: preparer,
		SRGBSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	if done, err := processor.ProcessOne(context.Background()); done || err == nil {
		t.Fatal("failed preview reported successful")
	}
	if done, err := processor.ProcessOne(context.Background()); done || err != nil {
		t.Fatal("preview cursor did not wrap", err)
	}
	preparer.fail = false
	if done, err := processor.ProcessOne(context.Background()); !done || err != nil ||
		preparer.calls != 2 || len(queue.cursors) != 3 || queue.cursors[1] != producer.Task ||
		queue.cursors[2] != uuid.Nil {
		t.Fatal("failed producer was not retried", err)
	}
}

func TestRestorationPreviewRefusesMissingBeforeDerivative(t *testing.T) {
	producer := RestorationPreviewProducer{Tenant: uuid.New(), Task: uuid.New()}
	run := uuid.New()
	preparer := &restorationPreviewPreparerFixture{receipt: RestorationPreviewReceipt{
		Run: run, SourceResult: uuid.New(),
		Before1600: restorationPreviewTaskID(run, "before", "preview1600"),
		After1600:  restorationPreviewTaskID(run, "after", "preview1600"),
		After2560:  restorationPreviewTaskID(run, "after", "preview2560"),
		State:      "running",
	}}
	processor := &RestorationPreviewProcessor{Source: &restorationPreviewQueueFixture{producer: producer},
		Preparer: preparer, SRGBSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	if done, err := processor.ProcessOne(context.Background()); done || err == nil {
		t.Fatal("incomplete before/after receipt accepted")
	}
}
