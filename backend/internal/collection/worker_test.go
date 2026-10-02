package collection

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestParseBulkCommandStrict(t *testing.T) {
	event, bulk := uuid.New(), uuid.New()
	command, err := ParseBulkCommand([]byte(`{"schema_version":1,"event_id":"` + event.String() + `","bulk_id":"` + bulk.String() + `"}`))
	if err != nil || command.EventID != event || command.BulkID != bulk {
		t.Fatalf("%+v %v", command, err)
	}
	for _, raw := range []string{
		`{}`,
		`{"schema_version":2,"event_id":"` + event.String() + `","bulk_id":"` + bulk.String() + `"}`,
		`{"schema_version":1,"event_id":"` + event.String() + `","bulk_id":"` + bulk.String() + `","extra":true}`,
		`{"schema_version":1,"schema_version":1,"event_id":"` + event.String() + `","bulk_id":"` + bulk.String() + `"}`,
		`{"schema_version":1,"event_id":"` + event.String() + `","bulk_id":"` + bulk.String() + `"} {}`,
	} {
		if _, err = ParseBulkCommand([]byte(raw)); !errors.Is(err, ErrPoisonBulkCommand) {
			t.Fatalf("accepted %s: %v", raw, err)
		}
	}
}

func TestBulkWorkerUsesEventFence(t *testing.T) {
	db := &bulkDB{row: bulkRow{value: true}}
	event, bulk := uuid.New(), uuid.New()
	body := []byte(`{"schema_version":1,"event_id":"` + event.String() + `","bulk_id":"` + bulk.String() + `"}`)
	changed, err := (BulkWorker{DB: db}).Process(context.Background(), body)
	if err != nil || !changed || db.query != "SELECT photo.process_bulk_chunk($1,$2)" {
		t.Fatalf("%v %v %q", changed, err, db.query)
	}
}

func TestMediaRetryCommandStrictAndDurable(t *testing.T) {
	event, bulk, item, version := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	body := []byte(`{"schema_version":1,"event_id":"` + event.String() + `","bulk_id":"` + bulk.String() + `","item_id":"` + item.String() + `","asset_version_id":"` + version.String() + `","variants":["thumbnail","preview"]}`)
	command, err := ParseMediaRetryCommand(body)
	if err != nil || command.EventID != event || len(command.Variants) != 2 {
		t.Fatal(command, err)
	}
	db := &bulkDB{row: bulkRow{value: true}}
	changed, err := (BulkWorker{DB: db}).Process(context.Background(), body)
	if err != nil || !changed || db.query != "SELECT photo.accept_media_retry_command($1,$2,$3,$4,$5)" || len(db.args) != 5 {
		t.Fatal(changed, err, db.query, db.args)
	}
	for _, raw := range []string{
		`{"schema_version":1,"event_id":"` + event.String() + `","bulk_id":"` + bulk.String() + `","item_id":"` + item.String() + `","asset_version_id":"` + version.String() + `","variants":["thumbnail","thumbnail"]}`,
		`{"schema_version":1,"event_id":"` + event.String() + `","bulk_id":"` + bulk.String() + `","item_id":"` + item.String() + `","asset_version_id":"` + version.String() + `","variants":["other"]}`,
	} {
		if _, err = ParseMediaRetryCommand([]byte(raw)); !errors.Is(err, ErrPoisonBulkCommand) {
			t.Fatal("accepted", raw, err)
		}
	}
}
