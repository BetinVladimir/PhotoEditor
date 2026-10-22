package semanticreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func shownRemovalTransportFixture(t *testing.T) (FrozenShownRemovalTask, PreparedExposureTask) {
	t.Helper()
	task := uuid.New()
	raw := json.RawMessage(`{"schema_version":1}`)
	sum := sha256.Sum256(raw)
	ref := v2input.Ref{ArtifactID: uuid.NewSHA1(task, []byte("Params")), SHA256: hex.EncodeToString(sum[:])}
	closure, err := v2input.FreezeInputInventory(task, "semantic.adopt_shown_removal", raw, ref, []v2input.Entry{{Ref: ref, Contract: "Params"}})
	if err != nil {
		t.Fatal(err)
	}
	frozen := FrozenShownRemovalTask{frozen: FrozenPreviewTask{draft: v2input.DraftTask{ID: task, Operation: "semantic.adopt_shown_removal", ExecutionProfile: json.RawMessage(`{"backend":"cpu"}`)}, params: ref, closure: closure}}
	published, err := PublishShownRemovalTask(context.Background(), frozen, &previewPublicationRecorder{})
	if err != nil {
		t.Fatal(err)
	}
	return frozen, published
}
func TestShownRemovalCommitProjectionRejectsCallerGraphAndProfileChanges(t *testing.T) {
	for _, change := range []string{"valid", "profile", "operation", "params", "transport", "direct", "body"} {
		frozen, published := shownRemovalTransportFixture(t)
		switch change {
		case "profile":
			published.Draft.ExecutionProfile = json.RawMessage(`{"backend":"cuda"}`)
		case "operation":
			published.Draft.Operation = "semantic.local_exposure"
		case "params":
			published.Params.ArtifactID = uuid.New()
		case "transport":
			published.Graph.Built.Manifest.SHA256 = published.Params.SHA256
		case "direct":
			published.Graph.Direct[0].ArtifactID = uuid.New()
		case "body":
			published.ParamsJCS = append(published.ParamsJCS, ' ')
		}
		payload, err := PrepareShownRemovalCommitInputs(context.Background(), frozen, published)
		if change == "valid" {
			if err != nil || len(payload.Inventory) == 0 || len(payload.Node) == 0 {
				t.Fatal(err)
			}
			continue
		}
		if err == nil || payload.Node != nil || payload.Inventory != nil {
			t.Fatal("tampered publication accepted", change)
		}
	}
}

func TestShownRemovalCommitProjectionIgnoresUntrustedCallerCursor(t *testing.T) {
	frozen, published := shownRemovalTransportFixture(t)
	called := false
	published.Graph.Open = func(context.Context) (v2input.Cursor, error) { called = true; return nil, ErrInvalid }
	payload, err := PrepareShownRemovalCommitInputs(context.Background(), frozen, published)
	if err != nil || called || len(payload.Transport) == 0 {
		t.Fatal("caller cursor consulted", err)
	}
	// SQL payload bytes own their memory; later mutation of the public projection
	// must not change the server's exact frozen document.
	payload.ParamsJCS[0] = 'x'
	if frozen.frozen.closure.ParamsJCS[0] != '{' {
		t.Fatal("frozen params aliased")
	}
}
