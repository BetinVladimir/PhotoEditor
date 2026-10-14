package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestNativeInputSelectionPinsRejectScopeMutation(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	ev := 0.0
	intent := v2input.SemanticEditIntent{SchemaVersion: 1, Kind: "local_exposure", Reference: v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(), CanvasHash: strings.Repeat("b", 64)}, ReferenceMask: ref(), TargetSnapshotID: uuid.New(), Strength: .5, EV: &ev, PreservePersons: true}
	body, _ := canonical.Marshal(intent)
	request, err := CompileRequest(uuid.New(), who.UserID, "key", body)
	if err != nil {
		t.Fatal(err)
	}
	reader := SQLTransferSourceReader{Bucket: "bucket", ProjectID: uuid.New()}
	proof := nativeInputSelectionSeparationProof{root: intent.ReferenceMask, pixels: ref(), count: 1}
	parent := &proof.registered.published.preparation.plan.prepared.dependencies.preparation.landmarksPlan.detector
	parent.request = request
	parent.prepared.source.project = reader.ProjectID
	parent.prepared.source.bucket = reader.Bucket
	parent.prepared.source.session = who.SessionID
	proof.rootObject = objectstore.Object{Key: "root", VersionID: "v1", SHA256: proof.root.SHA256, ContentType: "application/json", Bytes: 512}
	proof.pixelsObject = objectstore.Object{Key: "pixels", VersionID: "v2", SHA256: proof.pixels.SHA256, ContentType: "application/x-npy", Bytes: 528}
	if raw, err := reader.marshalNativeInputSelectionPins(who, request, proof); err != nil || len(raw) == 0 {
		t.Fatalf("valid pins refused: %v", err)
	}
	for _, kind := range []string{"snapshot", "tenant", "session", "empty", "version", "sha"} {
		p, r, actor := proof, request, who
		switch kind {
		case "snapshot":
			r.Intent.TargetSnapshotID = uuid.New()
		case "tenant":
			r.TenantID = uuid.New()
		case "session":
			actor.SessionID = uuid.New()
		case "empty":
			p.count = 0
		case "version":
			p.pixelsObject.VersionID = "null"
		case "sha":
			p.rootObject.SHA256 = strings.Repeat("c", 64)
		}
		if raw, err := reader.marshalNativeInputSelectionPins(actor, r, p); err == nil || raw != nil {
			t.Fatalf("accepted %s", kind)
		}
	}
}

func TestNativeInputSelectionLockCancelledBeforeTransaction(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := (SQLTransferSourceReader{}).lockNativeInputSelectionSeparation(ctx, nil, auth.Identity{}, Request{}, nativeInputSourceContinuation{}, nativeInputSelectionSeparationProof{}, TransferColorProfile{}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}
