package v2portrait

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type relightTransportRecorder struct {
	cancelManifest context.CancelFunc
	bodies         [][]byte
	alias          uuid.UUID
	fail           bool
}

func (p *relightTransportRecorder) Publish(ctx context.Context, task uuid.UUID, ordinal int, contract string, body []byte) (v2input.Ref, error) {
	if p.fail {
		return v2input.Ref{}, errors.New("publication failed")
	}
	p.bodies = append(p.bodies, append([]byte(nil), body...))
	sha, err := canonical.Hash(json.RawMessage(body))
	id := uuid.New()
	if p.alias != uuid.Nil && len(p.bodies) == 1 {
		id = p.alias
	}
	if contract == "InputClosureManifest" && p.cancelManifest != nil {
		p.cancelManifest()
	}
	return v2input.Ref{ArtifactID: id, SHA256: sha}, err
}

// Structural transport fixture only. Native bootstrap tests establish the
// private preparation through actual producer readers and SQL authority.
func relightFrozenFixture(t *testing.T, count int) (RelightBootstrapResult, artifactjson.Ref) {
	t.Helper()
	request := RelightBootstrapRequest{Workflow: uuid.New(), Ordinal: 3}
	entries := make([]v2input.Entry, count)
	for i := range entries {
		entries[i] = v2input.Entry{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}, Contract: "NpyArray"}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Ref.ArtifactID.String() < entries[j].Ref.ArtifactID.String() })
	inputs, _ := canonical.Marshal(entries)
	sha, _ := canonical.Hash(entries)
	owner, _ := v2input.TaskID(request.Workflow, "image.relight", request.Ordinal)
	intent := RelightIntent{OwnerID: owner, NodeKey: "image.relight:3", InputsJCS: string(inputs), InputsSHA: sha, ParamsSHA: strings.Repeat("b", 64), ProfileJCS: `{"backend":"cpu"}`, ReservedBytes: 1000}
	return RelightBootstrapResult{Intent: intent, prepared: &relightPreparedAuthority{request: request, intent: intent}}, artifactjson.Ref{ID: uuid.New(), SHA256: intent.ParamsSHA}
}

func TestFrozenRelightCompleteTransportAndOwnership(t *testing.T) {
	for _, count := range []int{13, 512} {
		prepared, ref := relightFrozenFixture(t, count)
		frozen, err := freezePreparedRelightTask(context.Background(), prepared, ref)
		if err != nil {
			t.Fatal(err)
		}
		draft, err := frozen.DraftTask()
		if err != nil || draft.ID != prepared.Intent.OwnerID || draft.Operation != "image.relight" || draft.OperationVersion != "1" || draft.ResourceClass != "cpu" || draft.ParamsArtifactID != ref.ID {
			t.Fatal("draft binding", err)
		}
		draft.ExecutionProfile[0] = '!'
		fresh, _ := frozen.DraftTask()
		if fresh.ExecutionProfile[0] != '{' {
			t.Fatal("borrowed profile")
		}
		publisher := &relightTransportRecorder{}
		graph, err := frozen.PublishInputClosure(context.Background(), publisher)
		if err != nil || graph.Built.Count != count+1 || len(graph.Built.Chunks) != (count+1+511)/512 {
			t.Fatal("complete transport", err)
		}
		var manifest v2input.Manifest
		if json.Unmarshal(publisher.bodies[len(publisher.bodies)-1], &manifest) != nil || manifest.TaskID != draft.ID || manifest.EntryCount != count+1 {
			t.Fatal("manifest")
		}
		graph.Direct[0] = v2input.Ref{}
		for pass := 0; pass < 2; pass++ {
			cursor, err := graph.Open(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			var entries []v2input.Entry
			paramsCount := 0
			for {
				entry, ok, err := cursor.Next(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				if !ok {
					break
				}
				entries = append(entries, entry)
				if entry.Ref.ArtifactID == ref.ID && entry.Contract == "Params" {
					paramsCount++
				}
			}
			sha, _ := canonical.Hash(entries)
			if len(entries) != count+1 || paramsCount != 1 || sha != manifest.EntriesSHA {
				t.Fatal("cursor lost producer inputs")
			}
			inputDigest, _ := canonical.Hash(map[string]any{"task": draft.ID, "operation": "image.relight", "version": "1", "entries": entries})
			if inputDigest != draft.InputDigest {
				t.Fatal("task input digest")
			}
		}
	}
}

func TestFrozenRelightRefusals(t *testing.T) {
	prepared, ref := relightFrozenFixture(t, 13)
	for _, mode := range []string{"unprepared", "intent", "digest", "owner", "params", "alias"} {
		p, r := prepared, ref
		w := *prepared.prepared
		p.prepared = &w
		switch mode {
		case "unprepared":
			p.prepared = nil
		case "intent":
			p.Intent.ReservedBytes++
		case "digest":
			w.intent.InputsSHA = strings.Repeat("c", 64)
			p.Intent = w.intent
		case "owner":
			w.request.Workflow = uuid.New()
		case "params":
			r.SHA256 = strings.Repeat("c", 64)
		case "alias":
			var entries []v2input.Entry
			_ = json.Unmarshal([]byte(p.Intent.InputsJCS), &entries)
			r.ID = entries[0].Ref.ArtifactID
		}
		if got, err := freezePreparedRelightTask(context.Background(), p, r); err == nil || got.entries != nil {
			t.Fatal("invalid frozen task", mode, err)
		}
	}
	frozen, err := freezePreparedRelightTask(context.Background(), prepared, ref)
	if err != nil {
		t.Fatal(err)
	}
	for _, publisher := range []*relightTransportRecorder{{fail: true}, {alias: ref.ID}} {
		if graph, err := frozen.PublishInputClosure(context.Background(), publisher); err == nil || graph.Open != nil {
			t.Fatal("partial graph returned", err)
		}
	}
	publicationCtx, cancelPublication := context.WithCancel(context.Background())
	defer cancelPublication()
	if graph, err := frozen.PublishInputClosure(publicationCtx, &relightTransportRecorder{cancelManifest: cancelPublication}); !errors.Is(err, context.Canceled) || graph.Open != nil {
		t.Fatal("cancelled manifest returned graph", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := frozen.OpenInputs(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if graph, err := frozen.PublishInputClosure(ctx, &relightTransportRecorder{}); !errors.Is(err, context.Canceled) || graph.Open != nil {
		t.Fatal(err)
	}
}
