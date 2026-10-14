package workflow

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// NodeDrainQueries must use the scheduler principal. The SQL functions own the
// row lock and prevent a concurrent dispatch from entering after drain begins.
type NodeDrainQueries interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type NodeDrainStatus struct {
	Node               string
	Generation         int64
	ActiveReservations int64
	OccupiedSlots      int64
	ReadyToStop        bool
}

type SQLNodeDrainer struct{ DB NodeDrainQueries }

// NodeStopper is installed by the operator, never by task payloads. It must
// revalidate the generation against the current node registration before
// invoking its infrastructure stop action.
type NodeStopper interface {
	StopNode(context.Context, string, int64) error
}

func validDrainNode(node string) bool { return len(node) >= 1 && len(node) <= 120 }

func (d SQLNodeDrainer) Begin(ctx context.Context, node string) (int64, error) {
	if d.DB == nil || !validDrainNode(node) {
		return 0, errors.New("invalid node drain request")
	}
	var generation int64
	if err := d.DB.QueryRow(ctx, "SELECT photo.begin_capacity_node_drain($1)", node).Scan(&generation); err != nil {
		return 0, err
	}
	if generation < 1 {
		return 0, errors.New("invalid node drain generation")
	}
	return generation, nil
}

func (d SQLNodeDrainer) Status(ctx context.Context, node string, generation int64) (NodeDrainStatus, error) {
	status := NodeDrainStatus{Node: node, Generation: generation}
	if d.DB == nil || !validDrainNode(node) || generation < 1 {
		return NodeDrainStatus{}, errors.New("invalid node drain proof")
	}
	err := d.DB.QueryRow(ctx,
		"SELECT active_reservations,occupied_slots,ready_to_stop FROM photo.capacity_node_drain_status($1,$2)",
		node, generation).Scan(&status.ActiveReservations, &status.OccupiedSlots, &status.ReadyToStop)
	if err != nil {
		return NodeDrainStatus{}, err
	}
	if status.ActiveReservations < 0 || status.OccupiedSlots < 0 ||
		status.ReadyToStop != (status.ActiveReservations == 0 && status.OccupiedSlots == 0) {
		return NodeDrainStatus{}, errors.New("contradictory node drain status")
	}
	return status, nil
}

// ReapExpired fences at most one task whose lease or deadline has expired on
// this draining node. A healthy running task is never killed by this method.
func (d SQLNodeDrainer) ReapExpired(ctx context.Context, node string, generation int64) (bool, error) {
	if d.DB == nil || !validDrainNode(node) || generation < 1 {
		return false, errors.New("invalid node drain proof")
	}
	var reaped int
	if err := d.DB.QueryRow(ctx,
		"SELECT photo.reap_capacity_node_drain_task($1,$2)", node, generation).Scan(&reaped); err != nil {
		return false, err
	}
	if reaped != 0 && reaped != 1 {
		return false, errors.New("invalid node drain reap count")
	}
	return reaped == 1, nil
}

// WaitReady observes a pinned generation. On timeout the caller must fence
// remaining tasks and repeat Status; a timeout alone never authorizes stop.
func (d SQLNodeDrainer) WaitReady(ctx context.Context, node string, generation int64, pollInterval time.Duration) (NodeDrainStatus, error) {
	if pollInterval <= 0 || pollInterval > 30*time.Second {
		return NodeDrainStatus{}, errors.New("invalid node drain poll interval")
	}
	deadline, bounded := ctx.Deadline()
	if !bounded || time.Until(deadline) > 7260*time.Second {
		return NodeDrainStatus{}, errors.New("node drain requires bounded deadline")
	}
	for {
		status, err := d.Status(ctx, node, generation)
		if err != nil || status.ReadyToStop {
			return status, err
		}
		if reaped, err := d.ReapExpired(ctx, node, generation); err != nil {
			return NodeDrainStatus{}, err
		} else if reaped {
			continue
		}
		timer := time.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return NodeDrainStatus{}, ctx.Err()
		case <-timer.C:
		}
	}
}

// StopReadyNode refuses to call an absent provisioner or to use a cached
// readiness result. A timeout from WaitReady is never a stop proof.
func (d SQLNodeDrainer) StopReadyNode(ctx context.Context, node string, generation int64, stopper NodeStopper) error {
	if stopper == nil {
		return errors.New("node stop provisioner unavailable")
	}
	status, err := d.Status(ctx, node, generation)
	if err != nil {
		return err
	}
	if !status.ReadyToStop {
		return errors.New("node drain still active")
	}
	return stopper.StopNode(ctx, node, generation)
}
