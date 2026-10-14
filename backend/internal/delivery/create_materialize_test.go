package delivery

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestCreatePrepareMaterializationBindsFrozenTask(t *testing.T) {
	intent, session, catalog, items := createDocumentsFixture(t)
	who := auth.Identity{UserID: intent.actor, SessionID: session}
	docs, err := intent.CompileDocuments(context.Background(), session, catalog, items)
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := docs.Plan.FreezeTask(docs.Plan.Roots(), PrepareResourceLimits{RAMBytes: 8 << 30, ScratchBytes: 8 << 30, OutputBytes: 8 << 20})
	if err != nil {
		t.Fatal(err)
	}
	tx := &proofTx{accepted: true}
	if err := intent.MaterializePrepare(context.Background(), tx, who, intent.tenant, frozen); err != nil {
		t.Fatal(err)
	}
	if len(tx.args) != 6 || tx.args[0] != intent.tenant || tx.args[1] != who.UserID || tx.args[2] != session || tx.args[3] != intent.key || tx.args[4] != intent.RequestSHA() {
		t.Fatal("scope changed")
	}
	var draft v2input.DraftTask
	if json.Unmarshal([]byte(tx.args[5].(string)), &draft) != nil || draft.ID != intent.Identity().PrepareTask || draft.ParamsArtifactID != intent.Identity().Params {
		t.Fatal("task changed")
	}
	tx.accepted = false
	if err := intent.MaterializePrepare(context.Background(), tx, who, intent.tenant, frozen); err == nil {
		t.Fatal("SQL refusal ignored")
	}
	tx.calls = 0
	if err := intent.MaterializePrepare(context.Background(), tx, who, uuid.New(), frozen); err == nil || tx.calls != 0 {
		t.Fatal("foreign scope queried")
	}
	if err := intent.MaterializePrepare(context.Background(), tx, who, intent.tenant, FrozenPrepareTask{}); err == nil || tx.calls != 0 {
		t.Fatal("empty task queried")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := intent.MaterializePrepare(ctx, tx, who, intent.tenant, frozen); err == nil || tx.calls != 0 {
		t.Fatal("cancelled IO")
	}
}
