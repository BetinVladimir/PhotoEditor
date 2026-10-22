package semanticreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"testing"
)

type previewPublicationRecorder struct {
	calls   []string
	failure string
	failAt int // zero preserves the original fail-on-every-call recorder
	cancel  context.CancelFunc
}

func (p *previewPublicationRecorder) Publish(ctx context.Context, task uuid.UUID, ordinal int, contract string, body []byte) (v2input.Ref, error) {
	p.calls = append(p.calls, contract)
	shouldFail := p.failAt == 0 || len(p.calls) == p.failAt
	if shouldFail && p.failure == "error" {
		return v2input.Ref{}, errors.New("storage unavailable")
	}
	sum := sha256.Sum256(body)
	ref := v2input.Ref{ArtifactID: uuid.NewSHA1(task, []byte(contract)), SHA256: hex.EncodeToString(sum[:])}
	if shouldFail && p.failure == "sha" {
		ref.SHA256 = ""
	}
	if shouldFail && p.failure == "identity" {
		ref.ArtifactID = uuid.New()
	}
	if shouldFail && p.cancel != nil {
		p.cancel()
	}
	return ref, nil
}
func TestPreviewPublicationPinsParamsBeforeManifest(t *testing.T) {
	task := uuid.New()
	raw := json.RawMessage(`{"schema_version":1}`)
	sum := sha256.Sum256(raw)
	params := v2input.Ref{ArtifactID: uuid.NewSHA1(task, []byte("Params")), SHA256: hex.EncodeToString(sum[:])}
	closure, err := v2input.FreezeInputInventory(task, "semantic.preview_local_exposure", raw, params, []v2input.Entry{{Ref: params, Contract: "Params"}})
	if err != nil {
		t.Fatal(err)
	}
	frozen := FrozenPreviewTask{draft: v2input.DraftTask{ID: task, ExecutionProfile: json.RawMessage(`{}`)}, params: params, closure: closure}
	for _, failure := range []string{"", "error", "sha", "identity", "cancel"} {
		t.Run(failure, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			publisher := &previewPublicationRecorder{failure: failure}
			if failure == "cancel" {
				publisher.cancel = cancel
			}
			result, err := PublishExposurePreviewTask(ctx, frozen, publisher)
			if failure != "" {
				if err == nil || result.Draft.ID != uuid.Nil || len(publisher.calls) != 1 {
					t.Fatal("partial publication escaped", err)
				}
				return
			}
			payload, payloadErr := PrepareExposurePreviewCommitInputs(context.Background(), frozen, result)
			if payloadErr != nil {
				t.Fatal("preview SQL projection", payloadErr)
			}
			var inventory ExposureInventoryBinding
			var transport ExposureTransportBinding
			var node v2input.DraftTask
			if json.Unmarshal(payload.Inventory, &inventory) != nil || json.Unmarshal(payload.Transport, &transport) != nil || json.Unmarshal(payload.Node, &node) != nil ||
				inventory.TaskID != task || transport.TaskID != task || node.ID != task || len(inventory.Entries) != 1 || inventory.Entries[0].Ref != params ||
				transport.EntriesSHA != result.Graph.Built.SHA256 || string(payload.ParamsJCS) != string(raw) {
				t.Fatal("SQL projection lost exact inventory")
			}
			changed := result
			changed.Graph.Built.Count++
			if bad, err := PrepareExposurePreviewCommitInputs(context.Background(), frozen, changed); err == nil || len(bad.Node) != 0 {
				t.Fatal("partial invalid SQL projection")
			}
			if err != nil || result.Params != params || len(publisher.calls) != 3 || publisher.calls[0] != "Params" || publisher.calls[1] != "InputClosureChunk" || publisher.calls[2] != "InputClosureManifest" {
				t.Fatal("wrong publication sequence", err, publisher.calls)
			}
		})
	}
}
