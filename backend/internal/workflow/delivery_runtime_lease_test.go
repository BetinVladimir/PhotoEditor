package workflow

import "testing"

func TestDeliveryLoopAdmissionRequiresCompletedHealthyPass(t *testing.T) {
	if deliveryLoopReady(nil) {
		t.Fatal("nil loop advertised delivery admission")
	}
	state := new(ExpansionLoopState)
	if deliveryLoopReady(state) {
		t.Fatal("unstarted loop advertised delivery admission")
	}
	state.Started.Store(true)
	if deliveryLoopReady(state) {
		t.Fatal("loop advertised delivery admission before first ProcessOne result")
	}
	state.Ready.Store(true)
	if !deliveryLoopReady(state) {
		t.Fatal("healthy completed pass did not enable delivery admission")
	}
	state.Ready.Store(false)
	state.Failures.Add(1)
	if deliveryLoopReady(state) {
		t.Fatal("failed pass kept delivery admission ready")
	}
	state.Ready.Store(true)
	state.Started.Store(false)
	if deliveryLoopReady(state) {
		t.Fatal("stopped loop kept delivery admission ready")
	}
}
