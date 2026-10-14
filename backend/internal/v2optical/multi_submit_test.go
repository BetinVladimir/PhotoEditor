package v2optical

import (
	"context"
	"errors"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"reflect"
	"testing"
)

type multiSubmitDouble struct {
	fault          string
	calls          []string
	receipt        MultiReceipt
	cancel         context.CancelFunc
	corruptRequest bool
}

var errMultiSubmitFault = errors.New("injected submission failure")

func (d *multiSubmitDouble) step(stage string, r MultiPrepareRequest) error {
	d.calls = append(d.calls, stage)
	if d.corruptRequest && (string(r.Params) != "params" || len(r.Sources) != 2 || string(r.Sources[0]) != "source" || string(r.Sources[1]) != "second") {
		return errors.New("retry request was mutated")
	}
	if len(r.Sources) > 0 && len(r.Sources[0]) > 0 {
		r.Sources[0][0] = '!'
		r.Sources[0] = nil
	}
	if len(r.Params) > 0 {
		r.Params[0] = '!'
	}
	if d.fault == stage {
		if d.cancel != nil {
			d.cancel()
		}
		return errMultiSubmitFault
	}
	return nil
}
func (d *multiSubmitDouble) PrepareParams(_ context.Context, _ auth.Identity, r MultiPrepareRequest) (PreparedMulti, error) {
	return PreparedMulti{}, d.step("prepare", r)
}
func (d *multiSubmitDouble) PublishTransport(_ context.Context, _ auth.Identity, r MultiPrepareRequest, _ PreparedMulti) (v2input.GraphTask, error) {
	return v2input.GraphTask{}, d.step("transport", r)
}
func (d *multiSubmitDouble) Admit(_ context.Context, _ auth.Identity, r MultiPrepareRequest, _ PreparedMulti) error {
	return d.step("admit", r)
}
func (d *multiSubmitDouble) ReadQueuedReceipt(_ context.Context, _ auth.Identity, r MultiPrepareRequest) (MultiReceipt, error) {
	return d.receipt, d.step("receipt", r)
}
func TestMultiSubmitRecoversWithoutSecondActivation(t *testing.T) {
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
			d := &multiSubmitDouble{fault: sample.fault, receipt: MultiReceipt{State: "queued"}, corruptRequest: true}
			r := MultiPrepareRequest{Params: []byte("params"), Sources: [][]byte{[]byte("source"), []byte("second")}}
			receipt, err := SubmitMulti(context.Background(), d, auth.Identity{}, r)
			if sample.fault == "receipt" {
				if !errors.Is(err, errMultiSubmitFault) {
					t.Fatal(err)
				}
			} else if err != nil || receipt.State != "queued" {
				t.Fatal(receipt, err)
			}
			if !reflect.DeepEqual(d.calls, sample.calls) || string(r.Params) != "params" || string(r.Sources[0]) != "source" || string(r.Sources[1]) != "second" {
				t.Fatal(d.calls, r)
			}
		})
	}
}
func TestMultiSubmitCancellationDoesNotRecoverOrReactivate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	d := &multiSubmitDouble{fault: "admit", cancel: cancel}
	if _, err := SubmitMulti(ctx, d, auth.Identity{}, MultiPrepareRequest{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d.calls, []string{"prepare", "transport", "admit"}) {
		t.Fatal(d.calls)
	}
}
