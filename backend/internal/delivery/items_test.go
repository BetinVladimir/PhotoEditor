package delivery

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"strings"
	"testing"
)

func TestDeliveryItemsScopeCursorAndOwnedResponse(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tid, id := uuid.New(), uuid.New()
	q := ItemsQuery{Revision: "2", AfterOrdinal: 3, AfterPreset: uuid.New(), Limit: 200}
	tx := &deliveryReadTx{raw: []byte(fmt.Sprintf(`{"package_id":%q,"revision":"2","manifest":null,"items":[],"next":null}`, id.String()))}
	result, err := ReadItems(context.Background(), tx, who, tid, id, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(tx.args) != 8 || tx.args[0] != tid || tx.args[1] != who.UserID || tx.args[2] != who.SessionID || tx.args[3] != id || tx.args[4] != int64(2) || tx.args[5] != 3 || tx.args[6] != q.AfterPreset || tx.args[7] != 200 {
		t.Fatal("scope/cursor changed", tx.args)
	}
	tx.raw[0] = 'x'
	if result[0] != '{' {
		t.Fatal("borrowed response")
	}
}
func TestDeliveryItemsInvalidAndStale(t *testing.T) {
	base := ItemsQuery{Revision: "1", AfterOrdinal: -1, Limit: 100}
	for _, q := range []ItemsQuery{{Revision: "01", AfterOrdinal: -1, Limit: 100}, {Revision: "9007199254740992", AfterOrdinal: -1, Limit: 100}, {Revision: "1", AfterOrdinal: -2, Limit: 100}, {Revision: "1", AfterOrdinal: 0, Limit: 100}, {Revision: "1", AfterOrdinal: -1, AfterPreset: uuid.New(), Limit: 100}, {Revision: "1", AfterOrdinal: -1, Limit: 201}} {
		tx := &deliveryReadTx{}
		if _, err := ReadItems(context.Background(), tx, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(), uuid.New(), q); err == nil || tx.calls != 0 {
			t.Fatal("invalid query reached SQL")
		}
	}
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	for _, raw := range []string{`null`, `{"stale":true}`, `broken`} {
		tx := &deliveryReadTx{raw: []byte(raw)}
		if _, err := ReadItems(context.Background(), tx, who, uuid.New(), uuid.New(), base); err == nil {
			t.Fatal("refusal ignored", raw)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tx := &deliveryReadTx{}
	if _, err := ReadItems(ctx, tx, who, uuid.New(), uuid.New(), base); err == nil || tx.calls != 0 {
		t.Fatal("cancelled query reached SQL")
	}
}

func TestDeliveryItemsPublicAllowlist(t *testing.T) {
	id := uuid.New()
	q := ItemsQuery{Revision: "1", AfterOrdinal: -1, Limit: 100}
	valid := fmt.Sprintf(`{"package_id":%q,"revision":"1","manifest":null,"items":[],"next":null}`, id.String())
	if !ValidItemsResource([]byte(valid), id, q) {
		t.Fatal("valid page rejected")
	}
	for _, raw := range []string{`{"items":[],"next":null}`, fmt.Sprintf(`{"package_id":%q,"revision":"1","manifest":null,"items":[],"next":null,"bucket":"private"}`, id.String()), fmt.Sprintf(`{"package_id":%q,"revision":"1","manifest":null,"items":[{"object_key":"private"}],"next":null}`, id.String())} {
		if ValidItemsResource([]byte(raw), id, q) {
			t.Fatal("private or unbound page accepted")
		}
	}
}

func TestDeliveryItemsOrderStateAndCursor(t *testing.T) {
	id, preset := uuid.New(), uuid.New()
	q := ItemsQuery{Revision: "1", AfterOrdinal: -1, Limit: 1}
	item := map[string]any{"id": uuid.New(), "ordinal": 0, "preset_id": preset, "preset_sha": strings.Repeat("a", 64), "required": true, "export_id": uuid.New(), "state": "ready", "reason": nil}
	page := map[string]any{"package_id": id, "revision": "1", "manifest": nil, "items": []any{item}, "next": map[string]any{"ordinal": 0, "preset_id": preset}}
	raw, _ := json.Marshal(page)
	if !ValidItemsResource(raw, id, q) {
		t.Fatal("valid cursor page rejected")
	}
	for _, change := range []map[string]any{{"ordinal": nil}, {"required": nil}, {"state": "unknown"}, {"export_id": nil}, {"reason": "unexpected"}, {"preset_sha": "bad"}} {
		copyItem := map[string]any{}
		for k, v := range item {
			copyItem[k] = v
		}
		for k, v := range change {
			copyItem[k] = v
		}
		page["items"] = []any{copyItem}
		raw, _ = json.Marshal(page)
		if ValidItemsResource(raw, id, q) {
			t.Fatal("invalid item accepted", change)
		}
	}
	page["items"] = []any{item}
	page["next"] = map[string]any{"ordinal": 1, "preset_id": preset}
	raw, _ = json.Marshal(page)
	if ValidItemsResource(raw, id, q) {
		t.Fatal("foreign cursor accepted")
	}
	page["next"] = nil
	q.AfterOrdinal = 0
	q.AfterPreset = preset
	raw, _ = json.Marshal(page)
	if ValidItemsResource(raw, id, q) {
		t.Fatal("cursor duplicate accepted")
	}
}

func TestDeliveryItemsMultiPresetStateMatrix(t *testing.T) {
	id := uuid.New()
	first := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	second := uuid.MustParse("00000000-0000-4000-8000-000000000002")
	for _, state := range []string{"pending", "ready", "failed", "omitted"} {
		t.Run(state, func(t *testing.T) {
			row := func(preset uuid.UUID) map[string]any {
				value := map[string]any{"id": uuid.New(), "ordinal": 0, "preset_id": preset, "preset_sha": strings.Repeat("a", 64), "required": false, "export_id": nil, "state": state, "reason": nil}
				if state == "ready" {
					value["export_id"] = uuid.New()
				}
				if state == "failed" || state == "omitted" {
					value["reason"] = "OUTPUT_UNAVAILABLE"
				}
				return value
			}
			rows := []any{row(first), row(second)}
			q := ItemsQuery{Revision: "1", AfterOrdinal: -1, Limit: 2}
			page := map[string]any{"package_id": id, "revision": "1", "manifest": nil, "items": rows, "next": map[string]any{"ordinal": 0, "preset_id": second}}
			raw, _ := json.Marshal(page)
			if !ValidItemsResource(raw, id, q) {
				t.Fatal("valid multi-preset state rejected", state)
			}
			page["items"] = []any{rows[1], rows[0]}
			raw, _ = json.Marshal(page)
			if ValidItemsResource(raw, id, q) {
				t.Fatal("reversed presets accepted")
			}
			page["items"] = []any{rows[1]}
			page["next"] = nil
			q.AfterOrdinal = 0
			q.AfterPreset = first
			raw, _ = json.Marshal(page)
			if !ValidItemsResource(raw, id, q) {
				t.Fatal("same ordinal continuation rejected")
			}
			q.AfterPreset = second
			if ValidItemsResource(raw, id, q) {
				t.Fatal("repeated continuation accepted")
			}
		})
	}
}

func TestDeliveryItemsOmittedHasNoRequiredExport(t *testing.T) {
	id := uuid.New()
	q := ItemsQuery{Revision: "1", AfterOrdinal: -1, Limit: 100}
	for _, required := range []bool{false, true} {
		for _, hasExport := range []bool{false, true} {
			var export any
			if hasExport {
				export = uuid.New()
			}
			row := map[string]any{"id": uuid.New(), "ordinal": 0, "preset_id": uuid.New(), "preset_sha": strings.Repeat("a", 64), "required": required, "export_id": export, "state": "omitted", "reason": "OPTIONAL_FAILED"}
			raw, err := json.Marshal(map[string]any{"package_id": id, "revision": "1", "manifest": nil, "items": []any{row}, "next": nil})
			if err != nil {
				t.Fatal(err)
			}
			if ValidItemsResource(raw, id, q) != (!required && !hasExport) {
				t.Fatalf("omission required=%v export=%v", required, hasExport)
			}
		}
	}
}

func TestDeliveryItemsRejectDuplicateKeysAtEveryLevel(t *testing.T) {
	id, preset, export := uuid.New(), uuid.New(), uuid.New()
	q := ItemsQuery{Revision: "1", AfterOrdinal: -1, Limit: 1}
	item := fmt.Sprintf(`{"id":%q,"ordinal":0,"preset_id":%q,"preset_sha":%q,"required":true,"export_id":%q,"state":"ready","reason":null}`, uuid.NewString(), preset.String(), strings.Repeat("a", 64), export.String())
	manifest := fmt.Sprintf(`{"artifact_id":%q,"sha256":%q}`, uuid.NewString(), strings.Repeat("b", 64))
	next := fmt.Sprintf(`{"ordinal":0,"preset_id":%q}`, preset.String())
	page := fmt.Sprintf(`{"package_id":%q,"revision":"1","manifest":%s,"items":[%s],"next":%s}`, id.String(), manifest, item, next)
	if !ValidItemsResource([]byte(page), id, q) {
		t.Fatal("valid nested page rejected")
	}
	for _, altered := range []string{
		strings.Replace(page, `"revision":"1"`, `"revision":"1","revision":"1"`, 1),
		strings.Replace(page, `"state":"ready"`, `"state":"ready","state":"ready"`, 1),
		strings.Replace(page, `"sha256":`, `"sha256":"`+strings.Repeat("b", 64)+`","sha256":`, 1),
		strings.Replace(page, `"next":{"ordinal":0`, `"next":{"ordinal":0,"ordinal":0`, 1),
	} {
		if altered == page || ValidItemsResource([]byte(altered), id, q) {
			t.Fatal("duplicate JSON key accepted", altered)
		}
	}
}
