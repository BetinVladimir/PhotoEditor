package queue

import (
	"context"
	"encoding/hex"
	"errors"
	"time"

	"github.com/google/uuid"
)

// RestoreTaskDispatcher keeps replay on the existing claim/confirm/mark path.
// Kind is always task; mail is intentionally handled by a separate authority.
type RestoreTaskDispatcher interface {
	Dispatch(context.Context, string, int) (int, error)
}

type RestoreReplayEvidence struct {
	Database         string
	InitiallyPending int64
	Confirmed        int64
	CompletedAt      time.Time
	sentInventorySHA string
	attestedDatabase string
	attestedCount    int64
	attestedAt       time.Time
	attested         bool
	boundRecoverySHA  string
	boundBuildSHA     string
	boundRunID        uuid.UUID
}

// ReplayRestoredTasks drains pending task outbox to an independently qualified
// isolated broker. The guard must verify target DB/broker identity, paused
// consumers and outgoing services, and exclusive recovery ownership throughout.
// Sent records are not reset: lost broker acknowledgements need independent
// delivery reconciliation before any later production cutover.
func (o *Outbox) ReplayRestoredTasks(ctx context.Context, expectedDatabase string,
	assertIsolated func(context.Context) error, maximumEvents int64) (RestoreReplayEvidence, error) {
	return o.replayRestoredTasks(ctx, expectedDatabase, assertIsolated, maximumEvents, nil, "")
}

func (o *Outbox) replayRestoredTasks(ctx context.Context, expectedDatabase string, assertIsolated func(context.Context) error, maximumEvents int64, expected *int64, expectedSHA string) (RestoreReplayEvidence, error) {
	if o == nil || o.DB == nil || o.Publisher == nil || expectedDatabase == "" || len(expectedDatabase) > 63 {
		return RestoreReplayEvidence{}, errors.New("restore replay installation unavailable")
	}
	if expected != nil {
		if err := o.verifyRestoreInventory(ctx, expectedDatabase, *expected, expectedSHA, assertIsolated); err != nil {
			return RestoreReplayEvidence{}, err
		}
	}
	var sentSince time.Time
	if expected != nil {
		if err := assertIsolated(ctx); err != nil {
			return RestoreReplayEvidence{}, err
		}
		var database string
		if err := o.DB.QueryRow(ctx, "SELECT current_database(),clock_timestamp()").Scan(&database, &sentSince); err != nil || database != expectedDatabase || sentSince.IsZero() {
			return RestoreReplayEvidence{}, errors.New("restore replay sent inventory clock unavailable")
		}
		if err := assertIsolated(ctx); err != nil {
			return RestoreReplayEvidence{}, err
		}
		emptyInventory := newRestoreEnvelopeInventory()
		if err := o.verifyRestoreSentInventory(ctx, expectedDatabase, sentSince, 0,
			hex.EncodeToString(emptyInventory.digest.Sum(nil)), assertIsolated); err != nil {
			return RestoreReplayEvidence{}, err
		}
	}
	pending := func(ctx context.Context) (int64, error) {
		var database string
		var count int64
		err := o.DB.QueryRow(ctx, "SELECT current_database(),photo.restore_task_outbox_pending($1)", expectedDatabase).Scan(&database, &count)
		if err != nil {
			return 0, err
		}
		if database != expectedDatabase {
			return 0, errors.New("restore replay database changed")
		}
		return count, nil
	}
	evidence, err := drainRestoredTasks(ctx, o, pending, assertIsolated, expectedDatabase, maximumEvents, expected)
	if err != nil {
		return RestoreReplayEvidence{}, err
	}
	if expected != nil {
		if err := o.verifyRestoreSentInventory(ctx, expectedDatabase, sentSince, *expected, expectedSHA, assertIsolated); err != nil {
			return RestoreReplayEvidence{}, err
		}
		evidence.sentInventorySHA = expectedSHA
		evidence.attestedDatabase = evidence.Database
		evidence.attestedCount = evidence.Confirmed
		evidence.attestedAt = evidence.CompletedAt
		evidence.attested = true
	}
	return evidence, nil
}

func drainRestoredTasks(ctx context.Context, dispatcher RestoreTaskDispatcher,
	pending func(context.Context) (int64, error), guard func(context.Context) error,
	database string, maximumEvents int64, expected ...*int64) (RestoreReplayEvidence, error) {
	empty := RestoreReplayEvidence{}
	if dispatcher == nil || pending == nil || guard == nil || maximumEvents < 1 || maximumEvents > 10_000_000 {
		return empty, errors.New("restore replay installation unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	check := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := guard(ctx); err != nil {
			return err
		}
		return ctx.Err()
	}
	if err := check(); err != nil {
		return empty, err
	}
	initial, err := pending(ctx)
	if err != nil {
		return empty, err
	}
	if len(expected) > 1 {
		return empty, errors.New("restore replay expected count invalid")
	}
	if len(expected) == 1 && expected[0] != nil && (*expected[0] < 0 || *expected[0] > maximumEvents || initial != *expected[0]) {
		return empty, errors.New("restore replay recovery count mismatch")
	}
	if initial < 0 || initial > maximumEvents {
		return empty, errors.New("restore replay event budget exceeded")
	}
	var confirmed int64
	for {
		if err := check(); err != nil {
			return empty, err
		}
		remaining, err := pending(ctx)
		if err != nil {
			return empty, err
		}
		if remaining < 0 || remaining+confirmed > initial {
			return empty, errors.New("restore replay outbox changed")
		}
		if remaining == 0 {
			if confirmed != initial {
				return empty, errors.New("restore replay acknowledgement mismatch")
			}
			if err := check(); err != nil {
				return empty, err
			}
			return RestoreReplayEvidence{Database: database, InitiallyPending: initial,
				Confirmed: confirmed, CompletedAt: time.Now().UTC()}, nil
		}
		limit := 100
		if remaining < int64(limit) {
			limit = int(remaining)
		}
		sent, err := dispatcher.Dispatch(ctx, "task", limit)
		if err != nil {
			return empty, err
		}
		if sent < 1 || sent > limit {
			return empty, errors.New("restore replay stalled or invalid receipt")
		}
		confirmed += int64(sent)
		if confirmed > maximumEvents {
			return empty, errors.New("restore replay event budget exceeded")
		}
	}
}
