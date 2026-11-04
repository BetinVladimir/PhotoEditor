package videoapi

import (
	"context"
	"io"
	"log"
	"net/http"

	"github.com/google/uuid"
	"photoeditor/backend/internal/domain"
)

// FN-Q-11 bounds (docs/video/20-saas-scaling-operations.md, 3.1).
const (
	egressLeaseMin = 8 << 20
	egressLeaseMax = 256 << 20
)

// EgressLeaseBytes sizes one lease: an even share of what remains between the open streams and the one asking, kept
// within [egressLeaseMin; egressLeaseMax] and never above what remains. Vector `egress_lease` (scale.json).
func EgressLeaseBytes(remaining int64, streams int) (int64, error) {
	if streams < 0 {
		return 0, domain.NewError("STREAMS_INVALID", 400)
	}
	if remaining <= 0 {
		return 0, nil
	}
	n := remaining / int64(streams+1)
	n = min(max(n, egressLeaseMin), egressLeaseMax)
	return min(n, remaining), nil
}

// leasedWriter passes body bytes only while a lease covers them and takes the next lease (FN-D-19) when one is used up,
// so a long stream holds at most one slice of the budget at a time. Used leases are settled at once, the last by close.
type leasedWriter struct {
	http.ResponseWriter
	ctx     context.Context
	svc     *Service
	v       Viewer
	render  uuid.UUID
	streams int   // other streams open on the share, for the lease size
	need    int64 // bytes the response still has to send
	cur     egressLease
	left    int64 // granted and not yet written on cur
	n       int64
	haveCur bool
}

func (w *leasedWriter) next() error {
	if w.haveCur {
		w.settle(w.cur.Granted)
		w.haveCur = false
	}
	want, err := EgressLeaseBytes(w.need, w.streams)
	if err != nil {
		return err
	}
	if want == 0 {
		return io.ErrShortWrite
	}
	l, err := w.svc.leaseEgress(w.ctx, w.v, want)
	if err != nil {
		return err
	}
	if l.Granted == 0 { // a lease for nothing still has to be returned
		w.settle0(l)
		return io.ErrShortWrite
	}
	w.cur, w.left, w.haveCur = l, l.Granted, true
	return nil
}

func (w *leasedWriter) settle(used int64) {
	if err := w.svc.settleEgress(w.ctx, w.v, w.cur, used, w.render); err != nil {
		log.Printf("video egress settle: %v", err)
	}
}

func (w *leasedWriter) settle0(l egressLease) {
	if err := w.svc.settleEgress(w.ctx, w.v, l, 0, w.render); err != nil {
		log.Printf("video egress settle: %v", err)
	}
}

func (w *leasedWriter) Write(p []byte) (int, error) {
	done := 0
	for len(p) > 0 {
		if !w.haveCur || w.left == 0 {
			if err := w.next(); err != nil {
				return done, err
			}
		}
		chunk := p
		if int64(len(chunk)) > w.left {
			chunk = chunk[:w.left]
		}
		n, err := w.ResponseWriter.Write(chunk)
		w.left -= int64(n)
		w.need -= int64(n)
		w.n += int64(n)
		done += n
		p = p[n:]
		if err != nil {
			return done, err
		}
	}
	return done, nil
}

// close settles the lease still held with what was actually written on it.
func (w *leasedWriter) close() {
	if w.haveCur {
		w.settle(w.cur.Granted - w.left)
		w.haveCur = false
	}
}

func (w *leasedWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
