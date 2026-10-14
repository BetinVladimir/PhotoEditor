package delivery

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"strings"
	"testing"
)

type signingReadinessTx struct {
	queries      []string
	signingReady bool
	signingArgs  []any
}

func (t *signingReadinessTx) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	t.queries = append(t.queries, query)
	if strings.Contains(query, "begin_v30_delivery_create_run_bound") {
		return proofRow{false}
	}
	if strings.Contains(query, "v30_delivery_signing_runtime_ready") {
		t.signingArgs = append([]any(nil), args...)
		return proofRow{t.signingReady}
	}
	return proofRow{true}
}

func TestCreateDraftRunBindsPendingAndCurrentScope(t *testing.T) {
	intent, session, catalog, items := createDocumentsFixture(t)
	who := auth.Identity{UserID: intent.actor, SessionID: session}
	raw, err := intent.EncodePending(context.Background(), session, catalog, items)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := intent.DecodePending(context.Background(), session, raw)
	if err != nil {
		t.Fatal(err)
	}
	tx := &proofTx{accepted: true}
	if err := intent.BeginRun(context.Background(), tx, who, intent.tenant, pending, "photos"); err != nil {
		t.Fatal(err)
	}
	if len(tx.args) != 7 || tx.args[0] != intent.tenant || tx.args[1] != who.UserID || tx.args[2] != session || tx.args[3] != intent.key || tx.args[4] != intent.RequestSHA() || tx.args[6] != "photos" {
		t.Fatal("scope mismatch", tx.args)
	}
	var config map[string]any
	if json.Unmarshal([]byte(tx.args[5].(string)), &config) != nil || config["project_id"] != intent.Request().ProjectID.String() || config["workflow_type"] != "delivery" || config["queue_class"] != "batch" {
		t.Fatal("invalid config")
	}
	tx.accepted = false
	if err := intent.BeginRun(context.Background(), tx, who, intent.tenant, pending, "photos"); err == nil {
		t.Fatal("SQL refusal ignored")
	}
	tx.calls = 0
	if err := intent.BeginRun(context.Background(), tx, who, intent.tenant, pending, "ab"); err == nil || tx.calls != 0 {
		t.Fatal("invalid bucket reached SQL")
	}
	if err := intent.BeginRun(context.Background(), tx, who, uuid.New(), pending, "photos"); err == nil || tx.calls != 0 {
		t.Fatal("foreign scope queried")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := intent.BeginRun(ctx, tx, who, intent.tenant, pending, "photos"); err == nil || tx.calls != 0 {
		t.Fatal("cancelled IO")
	}
}

func TestCreateRunPreservesHistoricalPendingQueueOmission(t *testing.T) {
	intent, session, catalog, items := createDocumentsFixture(t)
	ctx := context.Background()
	raw, err := intent.EncodePending(ctx, session, catalog, items)
	if err != nil {
		t.Fatal(err)
	}
	var historical map[string]json.RawMessage
	if err := json.Unmarshal(raw, &historical); err != nil {
		t.Fatal(err)
	}
	delete(historical, "queue_class")
	oldRaw, err := json.Marshal(historical)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := intent.DecodePending(ctx, session, oldRaw)
	if err != nil {
		t.Fatal(err)
	}
	tx := &proofTx{accepted: true}
	who := auth.Identity{UserID: intent.actor, SessionID: session}
	if err := intent.BeginRun(ctx, tx, who, intent.tenant, pending, "photos"); err != nil {
		t.Fatal(err)
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal([]byte(tx.args[5].(string)), &config); err != nil {
		t.Fatal(err)
	}
	if _, present := config["queue_class"]; present {
		t.Fatal("historical config changed")
	}
	first := tx.args[5].(string)
	if err := intent.BeginRun(ctx, tx, who, intent.tenant, pending, "photos"); err != nil {
		t.Fatal(err)
	}
	if tx.args[5].(string) != first {
		t.Fatal("replay config bytes changed")
	}
	for _, foreign := range []string{`"interactive"`, `"training"`, `null`, `""`} {
		historical["queue_class"] = json.RawMessage(foreign)
		invalid, err := json.Marshal(historical)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := intent.DecodePending(ctx, session, invalid); err == nil {
			t.Fatalf("accepted foreign queue %s", foreign)
		}
	}
	pending.QueueClass = "interactive"
	tx.calls = 0
	if err := intent.BeginRun(ctx, tx, who, intent.tenant, pending, "photos"); err == nil || tx.calls != 0 {
		t.Fatal("foreign queue reached SQL")
	}
}

func TestCreateRunRequiresTenantSigningRuntimeForRequiredPolicy(t *testing.T) {
	base, session, catalog, items := createDocumentsFixture(t)
	request := base.Request()
	request.Policy.Provenance = "required"
	request.Policy.SigningPolicyID = uuid.New()
	request.Policy.SigningPolicySHA = strings.Repeat("a", 64)
	who := auth.Identity{UserID: base.actor, SessionID: session}
	intent, err := FreezeCreateIntent(base.tenant, who, request, base.key)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := intent.EncodePending(context.Background(), session, catalog, items)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := intent.DecodePending(context.Background(), session, raw)
	if err != nil {
		t.Fatal(err)
	}
	tx := &signingReadinessTx{}
	err = intent.BeginRun(context.Background(), tx, who, intent.tenant, pending, "photos")
	if err == nil || len(tx.queries) != 3 || !strings.Contains(tx.queries[2], "v30_delivery_signing_runtime_ready") || len(tx.signingArgs) != 4 || tx.signingArgs[0] != "photos" || tx.signingArgs[1] != intent.tenant || tx.signingArgs[2] != request.Policy.SigningPolicyID || tx.signingArgs[3] != request.Policy.SigningPolicySHA {
		t.Fatal("missing signing readiness classification", err, tx.queries)
	}
}
