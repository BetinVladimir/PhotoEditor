package v2optical

import (
	"context"
	"errors"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"reflect"
	"testing"
)

type perspectiveSubmitDouble struct {
	fault          string
	calls          []string
	receipt        PerspectiveReceipt
	cancel         context.CancelFunc
	corruptRequest bool
}

var errPerspectiveSubmitFault = errors.New("injected submission failure")

func (d *perspectiveSubmitDouble) step(stage string, r PerspectivePrepareRequest) error {
	d.calls = append(d.calls, stage)
	if d.corruptRequest && (string(r.Params) != "params" || string(r.Source) != "source") {
		return errors.New("retry request was mutated")
	}
	if len(r.Params) > 0 {
		r.Params[0] = '!'
	}
	if d.fault == stage {
		if d.cancel != nil {
			d.cancel()
		}
		return errPerspectiveSubmitFault
	}
	return nil
}
func (d *perspectiveSubmitDouble) PrepareParams(_ context.Context, _ auth.Identity, r PerspectivePrepareRequest) (PreparedPerspective, error) {
	return PreparedPerspective{}, d.step("prepare", r)
}
func (d *perspectiveSubmitDouble) PublishTransport(_ context.Context, _ auth.Identity, r PerspectivePrepareRequest, _ PreparedPerspective) (v2input.GraphTask, error) {
	return v2input.GraphTask{}, d.step("transport", r)
}
func (d *perspectiveSubmitDouble) Admit(_ context.Context, _ auth.Identity, r PerspectivePrepareRequest, _ PreparedPerspective) error {
	return d.step("admit", r)
}
func (d *perspectiveSubmitDouble) ReadQueuedReceipt(_ context.Context, _ auth.Identity, r PerspectivePrepareRequest) (PerspectiveReceipt, error) {
	return d.receipt, d.step("receipt", r)
}
func TestPerspectiveSubmitRecoversWithoutSecondActivation(t *testing.T) {
	for _, sample := range []struct {
		fault string
		calls []string
	}{
		{"", []string{"prepare", "transport", "admit", "receipt"}},
		{"prepare", []string{"prepare", "receipt"}},
		{"transport", []string{"prepare", "transport", "receipt"}},
		{"admit", []string{"prepare", "transport", "admit", "receipt"}},
		{"receipt", []string{"prepare", "transport", "admit", "receipt"}},
	} {
		t.Run(sample.fault, func(t *testing.T) {
			d := &perspectiveSubmitDouble{fault: sample.fault, receipt: PerspectiveReceipt{State: "queued"}, corruptRequest: true}
			r := PerspectivePrepareRequest{Params: []byte("params"), Source: []byte("source")}
			receipt, err := SubmitPerspective(context.Background(), d, auth.Identity{}, r)
			if sample.fault == "receipt" {
				if !errors.Is(err, errPerspectiveSubmitFault) {
					t.Fatal(err)
				}
			} else if err != nil || receipt.State != "queued" {
				t.Fatal(receipt, err)
			}
			if !reflect.DeepEqual(d.calls, sample.calls) || string(r.Params) != "params" {
				t.Fatal(d.calls, r)
			}
		})
	}
}
func TestPerspectiveSubmitCancellationDoesNotRecoverOrReactivate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	d := &perspectiveSubmitDouble{fault: "admit", cancel: cancel}
	if _, err := SubmitPerspective(ctx, d, auth.Identity{}, PerspectivePrepareRequest{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d.calls, []string{"prepare", "transport", "admit"}) {
		t.Fatal(d.calls)
	}
}
