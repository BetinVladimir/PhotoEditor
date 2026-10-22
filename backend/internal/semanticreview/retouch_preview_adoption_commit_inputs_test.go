package semanticreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func shownRetouchTransportFixture(t *testing.T, counts ...int) (FrozenShownRetouchTask, PreparedExposureTask) {
	t.Helper()
	task := uuid.New()
	raw := json.RawMessage(`{"schema_version":1}`)
	sum := sha256.Sum256(raw)
	ref := v2input.Ref{ArtifactID: uuid.NewSHA1(task, []byte("Params")), SHA256: hex.EncodeToString(sum[:])}
	entries := []v2input.Entry{{Ref: ref, Contract: "Params"}}
	if len(counts) == 1 {
		for i := 1; i < counts[0]; i++ {
			entries = append(entries, v2input.Entry{Ref: v2input.Ref{ArtifactID: uuid.NewSHA1(task, []byte(fmt.Sprintf("pixel-%d", i))), SHA256: ref.SHA256}, Contract: "NpyArray"})
		}
	}
	closure, err := v2input.FreezeInputInventory(task, "semantic.adopt_shown_retouch_region", raw, ref, entries)
	if err != nil {
		t.Fatal(err)
	}
	frozen := FrozenShownRetouchTask{frozen: FrozenPreviewTask{draft: v2input.DraftTask{ID: task, Operation: "semantic.adopt_shown_retouch_region", ExecutionProfile: json.RawMessage(`{"backend":"cpu"}`)}, params: ref, closure: closure}}
	published, err := PublishShownRetouchTask(context.Background(), frozen, &previewPublicationRecorder{})
	if err != nil {
		t.Fatal(err)
	}
	return frozen, published
}

func TestShownRetouchMaximumTransportKeepsParamsInFullInventory(t *testing.T) {
	for _, count := range []int{128, 511, 512} {
		frozen, published := shownRetouchTransportFixture(t, count)
		payload, err := PrepareShownRetouchCommitInputs(context.Background(), frozen, published)
		if err != nil {
			t.Fatal(err)
		}
		var transport ExposureTransportBinding
		var inventory ExposureInventoryBinding
		if json.Unmarshal(payload.Transport, &transport) != nil || json.Unmarshal(payload.Inventory, &inventory) != nil {
			t.Fatal("invalid projection")
		}
		wantRoots := count
		if count == 512 {
			wantRoots--
		}
		if transport.EntryCount != count || len(inventory.Entries) != count || len(transport.DirectRoots) != wantRoots || len(transport.DirectRoots)+1 > 512 {
			t.Fatal("inventory lost or envelope exceeded", count)
		}
		paramsCount := 0
		for _, entry := range inventory.Entries {
			if entry.Ref == published.Params && entry.Contract == "Params" {
				paramsCount++
			}
		}
		if paramsCount != 1 || len(published.Graph.Direct) != count {
			t.Fatal("full frozen inventory mutated")
		}
		for _, ref := range transport.DirectRoots {
			if count == 512 && ref == published.Params {
				t.Fatal("redundant Params at wire limit")
			}
		}
	}
}
func TestShownRetouchCommitProjectionRejectsCallerGraphAndProfileChanges(t *testing.T) {
	for _, change := range []string{"valid", "profile", "operation", "params", "transport", "direct", "body"} {
		frozen, published := shownRetouchTransportFixture(t)
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
		payload, err := PrepareShownRetouchCommitInputs(context.Background(), frozen, published)
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

func TestShownRetouchCommitProjectionIgnoresUntrustedCallerCursor(t *testing.T) {
	frozen, published := shownRetouchTransportFixture(t)
	called := false
	published.Graph.Open = func(context.Context) (v2input.Cursor, error) { called = true; return nil, ErrInvalid }
	payload, err := PrepareShownRetouchCommitInputs(context.Background(), frozen, published)
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
