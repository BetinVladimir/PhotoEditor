package queue

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
)

type restoreEnvelopeInventory struct {
	digest   hash.Hash
	previous string
	count    int64
}

// The task publisher claims only these exchanges and refuses oversized
// payloads. Reject an unpublishable inventory before any broker mutation.
func restoreTaskEnvelopePublishable(destination string, payload []byte) bool {
	return (destination == Commands || destination == Events) && len(payload) <= 65536
}

func newRestoreEnvelopeInventory() *restoreEnvelopeInventory {
	d := sha256.New()
	d.Write([]byte("photoeditor.restore-task-outbox/v1\n"))
	return &restoreEnvelopeInventory{digest: d}
}
func (i *restoreEnvelopeInventory) append(id, tenant, event uuid.UUID, destination, routing string, payload []byte) error {
	if id == uuid.Nil || tenant == uuid.Nil || event == uuid.Nil || (i.previous != "" && id.String() <= i.previous) || destination == "" || len(destination) > 4096 || len(routing) > 4096 || len(payload) > 1<<20 || i.count >= 10_000_000 {
		return errors.New("restore outbox envelope invalid")
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(payload, &object) != nil || object == nil {
		return errors.New("restore outbox payload invalid")
	}
	raw, err := canonical.Marshal(map[string]any{"id": id.String(), "tenant_id": tenant.String(), "event_id": event.String(), "destination": destination, "routing_key": routing, "payload": json.RawMessage(payload)})
	if err != nil || len(raw) > 1<<20 {
		return errors.New("restore outbox envelope encoding")
	}
	i.digest.Write(raw)
	i.digest.Write([]byte{'\n'})
	i.previous = id.String()
	i.count++
	return nil
}

func (o *Outbox) verifyRestoreInventory(ctx context.Context, database string, expected int64, sha string, guard func(context.Context) error) error {
	return o.verifyRestoreInventoryPage(ctx, database, expected, sha, guard, nil)
}

func (o *Outbox) verifyRestoreSentInventory(ctx context.Context, database string, since time.Time, expected int64, sha string, guard func(context.Context) error) error {
	if since.IsZero() {
		return errors.New("restore sent inventory clock invalid")
	}
	return o.verifyRestoreInventoryPage(ctx, database, expected, sha, guard, &since)
}

func (o *Outbox) verifyRestoreInventoryPage(ctx context.Context, database string, expected int64, sha string, guard func(context.Context) error, since *time.Time) error {
	if o == nil || o.DB == nil || database == "" || expected < 0 || expected > 10_000_000 || guard == nil || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(sha) {
		return errors.New("restore inventory installation invalid")
	}
	if err := guard(ctx); err != nil {
		return err
	}
	tx, err := o.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SET LOCAL statement_timeout='30s'"); err != nil {
		return err
	}
	inventory := newRestoreEnvelopeInventory()
	var after any
	for {
		if err = guard(ctx); err != nil {
			return err
		}
		var rows pgx.Rows
		var queryErr error
		if since == nil {
			rows, queryErr = tx.Query(ctx, "SELECT id,tenant_id,event_id,destination,routing_key,payload FROM photo.restore_task_outbox_page($1,$2,128)", database, after)
		} else {
			rows, queryErr = tx.Query(ctx, "SELECT id,tenant_id,event_id,destination,routing_key,payload FROM photo.restore_task_outbox_sent_page($1,$2,$3,128)", database, *since, after)
		}
		if queryErr != nil {
			return queryErr
		}
		n := 0
		for rows.Next() {
			var id, tenant, event uuid.UUID
			var destination, routing string
			var payload []byte
			if err = rows.Scan(&id, &tenant, &event, &destination, &routing, &payload); err == nil && !restoreTaskEnvelopePublishable(destination, payload) {
				err = errors.New("restore outbox envelope not publishable")
			}
			if err == nil {
				err = inventory.append(id, tenant, event, destination, routing, payload)
			}
			if err == nil {
				err = ctx.Err()
			}
			if err != nil {
				rows.Close()
				return err
			}
			after = id
			n++
			if inventory.count > expected {
				rows.Close()
				return errors.New("restore inventory count changed")
			}
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
		if n < 128 {
			break
		}
	}
	if inventory.count != expected || hex.EncodeToString(inventory.digest.Sum(nil)) != sha {
		return errors.New("restore inventory digest mismatch")
	}
	if err = guard(ctx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
