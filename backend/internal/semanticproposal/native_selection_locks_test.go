package semanticproposal

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestNativeSelectionPinsBindParentIntentAndExactObjects(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	ev := 0.0
	intent := v2input.SemanticEditIntent{SchemaVersion: 1, Kind: "local_exposure", Reference: v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(), CanvasHash: strings.Repeat("b", 64)}, ReferenceMask: ref(), TargetSnapshotID: uuid.New(), Strength: .5, EV: &ev, PreservePersons: true}
	body, _ := canonical.Marshal(intent)
	request, err := CompileRequest(uuid.New(), who.UserID, "key", body)
	if err != nil {
		t.Fatal(err)
	}
	sha, _ := canonical.Hash(intent)
	anchor, _ := canonical.Marshal(map[string]any{"intent": v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}, "source": intent.Reference, "parent_run_id": request.RunID, "request_sha": request.RequestSHA, "auth_session_id": who.SessionID})
	proof := nativeSelectionSeparationProof{root: intent.ReferenceMask, pixels: ref(), count: 1, registered: registeredNativeProtectionProof{parent: nativeDefectParentProof{anchor: string(anchor)}}}
	proof.rootObject = objectstore.Object{Key: "root", VersionID: "root-version", SHA256: proof.root.SHA256, ContentType: "application/json", Bytes: 512}
	proof.pixelsObject = objectstore.Object{Key: "pixels", VersionID: "pixels-version", SHA256: proof.pixels.SHA256, ContentType: "application/x-npy", Bytes: 528}
	reader := SQLTransferSourceReader{Bucket: "bucket", ProjectID: uuid.New()}
	raw, err := reader.marshalNativeSelectionPins(who, request, proof)
	if err != nil {
		t.Fatal(err)
	}
	var pins []map[string]any
	if json.Unmarshal(raw, &pins) != nil || len(pins) != 2 || pins[0]["version"] != "root-version" || pins[1]["version"] != "pixels-version" || pins[0]["parent_id"] != nil || pins[1]["parent_id"] != proof.root.ArtifactID.String() {
		t.Fatal("lost exact pin or parent", string(raw))
	}
	for _, kind := range []string{"empty", "sha", "session", "snapshot", "root", "version", "media"} {
		changed, req, actor := proof, request, who
		switch kind {
		case "empty":
			changed.count = 0
		case "sha":
			changed.pixelsObject.SHA256 = strings.Repeat("c", 64)
		case "session":
			actor.SessionID = uuid.New()
		case "snapshot":
			req.Intent.TargetSnapshotID = uuid.New()
		case "root":
			changed.root = ref()
		case "version":
			changed.rootObject.VersionID = "null"
		case "media":
			changed.pixelsObject.ContentType = "application/json"
		}
		if raw, err := reader.marshalNativeSelectionPins(actor, req, changed); err == nil || raw != nil {
			t.Fatalf("accepted %s", kind)
		}
	}
}
