package delivery

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"strings"
	"testing"
)

func createCatalogFixture(t *testing.T) (CreateIntent, auth.Identity, uuid.UUID, []byte) {
	t.Helper()
	_, _, policy, configs := freezeFixture(t)
	tid := uuid.New()
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	request := CreateRequest{ProjectID: uuid.New(), SnapshotID: uuid.New(), Name: "Delivery", Policy: policy}
	intent, err := FreezeCreateIntent(tid, who, request, "key")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{"snapshot_id": request.SnapshotID, "snapshot_digest": strings.Repeat("a", 64), "item_count": 1, "presets": []any{map[string]any{"id": configs[0].ID, "revision": "1", "sha256": configs[0].SHA, "config": json.RawMessage(configs[0].Raw)}}})
	if err != nil {
		t.Fatal(err)
	}
	return intent, who, tid, raw
}
func TestCreateCatalogScopeAndCanonicalBytes(t *testing.T) {
	intent, who, tid, raw := createCatalogFixture(t)
	tx := &deliveryReadTx{raw: raw}
	value, err := intent.ResolveCatalog(context.Background(), tx, who, tid)
	if err != nil || value.ItemCount != 1 || len(value.Presets) != 1 {
		t.Fatal(value, err)
	}
	if len(tx.args) != 6 || tx.args[0] != tid || tx.args[1] != intent.Request().ProjectID || tx.args[2] != intent.Request().SnapshotID || tx.args[3] != who.UserID || tx.args[4] != who.SessionID {
		t.Fatal("scope changed", tx.args)
	}
	if digest(value.Presets[0].ConfigJCS) != value.Presets[0].SHA256 {
		t.Fatal("unverified config")
	}
	raw[0] = 'x'
	if !json.Valid(value.Presets[0].ConfigJCS) {
		t.Fatal("borrowed bytes")
	}
	tx.calls = 0
	if _, err := intent.ResolveCatalog(context.Background(), tx, who, uuid.New()); err == nil || tx.calls != 0 {
		t.Fatal("foreign tenant queried")
	}
	who.UserID = uuid.New()
	if _, err := intent.ResolveCatalog(context.Background(), tx, who, tid); err == nil || tx.calls != 0 {
		t.Fatal("foreign actor queried")
	}
}
func TestCreateCatalogRefusesCorruptionAndStale(t *testing.T) {
	intent, who, tid, raw := createCatalogFixture(t)
	for _, field := range []string{"snapshot_id", "snapshot_digest", "item_count", "id", "sha256", "revision"} {
		var wire map[string]any
		_ = json.Unmarshal(raw, &wire)
		switch field {
		case "snapshot_id":
			wire[field] = uuid.New().String()
		case "snapshot_digest":
			wire[field] = "broken"
		case "item_count":
			wire[field] = 0
		default:
			preset := wire["presets"].([]any)[0].(map[string]any)
			switch field {
			case "id":
				preset[field] = uuid.New().String()
			case "sha256":
				preset[field] = strings.Repeat("f", 64)
			case "revision":
				preset[field] = "01"
			}
		}
		changed, _ := json.Marshal(wire)
		if _, err := intent.DecodeCatalog(changed); err == nil {
			t.Fatal("corruption accepted", field)
		}
	}
	tx := &deliveryReadTx{raw: []byte("null")}
	if _, err := intent.ResolveCatalog(context.Background(), tx, who, tid); err == nil {
		t.Fatal("stale accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tx.calls = 0
	if _, err := intent.ResolveCatalog(ctx, tx, who, tid); err == nil || tx.calls != 0 {
		t.Fatal("cancelled IO")
	}
}

func TestCreateCatalogRefusesUnknownAndTrailingFields(t *testing.T) {
	intent, _, _, raw := createCatalogFixture(t)
	for _, mode := range []string{"top", "preset", "trailing", "oversize"} {
		var changed []byte
		switch mode {
		case "trailing": changed=append(append([]byte(nil),raw...),[]byte(` {}`)...)
		case "oversize": changed=make([]byte,(8<<20)+1)
		default:
			var wire map[string]any
			if err:=json.Unmarshal(raw,&wire);err!=nil{t.Fatal(err)}
			if mode=="top"{wire["unexpected"]=true}else{wire["presets"].([]any)[0].(map[string]any)["unexpected"]=true}
			var err error;changed,err=json.Marshal(wire);if err!=nil{t.Fatal(err)}
		}
		if _,err:=intent.DecodeWatermarkCatalog(changed);err==nil{t.Fatal("open catalog accepted",mode)}
	}
}
