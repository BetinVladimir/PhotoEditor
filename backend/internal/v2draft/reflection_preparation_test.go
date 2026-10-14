package v2draft

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestReflectionPreparationAcceptsOnlyDistinctEditorEvidence(t *testing.T) {
	ref := func() v2input.Ref {
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	}
	request := ReflectionPreparationRequest{SchemaVersion: 1,
		Snapshot: uuid.New(), Ordinal: 4,
		Base: v2input.SessionVersionBinding{AssetVersionID: uuid.New(),
			Source: ref(), CanvasHash: strings.Repeat("b", 64)},
		Strength: .5, Mask: ref(), Protected: ref(),
		TargetPixel: ref(), ProtectedPixel: ref()}
	body, err := canonical.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := DecodeReflectionPreparationRequest(body); err != nil || got != request {
		t.Fatal("valid manual reflection selection refused", err)
	}
	for _, mutate := range []func(*ReflectionPreparationRequest){
		func(v *ReflectionPreparationRequest) { v.Mask = v.Protected },
		func(v *ReflectionPreparationRequest) { v.TargetPixel = v.ProtectedPixel },
		func(v *ReflectionPreparationRequest) { v.Base.Source = v.Mask },
		func(v *ReflectionPreparationRequest) { v.Strength = 2 },
		func(v *ReflectionPreparationRequest) { v.ProtectedPixel.SHA256 = "invalid" },
		func(v *ReflectionPreparationRequest) { v.Ordinal = 5000 },
	} {
		changed := request
		mutate(&changed)
		bad, err := canonical.Marshal(changed)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeReflectionPreparationRequest(bad); err != v2input.ErrInvalid {
			t.Fatal("invalid reflection evidence accepted", err)
		}
	}
	if _, err := DecodeReflectionPreparationRequest(append([]byte(" "), body...)); err != v2input.ErrInvalid {
		t.Fatal("noncanonical reflection selection accepted", err)
	}
	withPin := append([]byte(`{"qualification":{},`), body[1:]...)
	if _, err := DecodeReflectionPreparationRequest(withPin); err != v2input.ErrInvalid {
		t.Fatal("client-owned qualification accepted", err)
	}
	proposal, err := canonical.Marshal(map[string]any{"schema_version": 1,
		"base": request.Base, "strength": request.Strength, "mask": request.Mask,
		"protected": request.Protected, "model_lock_sha": strings.Repeat("c", 64),
		"calibration": ref()})
	if err != nil {
		t.Fatal(err)
	}
	intent := ReflectionClientIntent{SchemaVersion: 1, Snapshot: request.Snapshot,
		Ordinal: request.Ordinal, Proposal: proposal, TargetPixel: request.TargetPixel,
		ProtectedPixel: request.ProtectedPixel}
	intentRaw, err := canonical.Marshal(intent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeReflectionClientIntent(intentRaw); err != nil {
		t.Fatal("server-issued reflection intent refused", err)
	}
	intent.TargetPixel = intent.ProtectedPixel
	badIntent, err := canonical.Marshal(intent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeReflectionClientIntent(badIntent); err != v2input.ErrInvalid {
		t.Fatal("changed reflection intent pixel pair accepted", err)
	}
	service := ReflectionPreparationService{}
	if _, err := service.PrepareProjectKeyed(context.Background(), auth.Identity{
		UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(), uuid.New(),
		"reflection-prepare-1", request); err != v2input.ErrInvalid {
		t.Fatal("uninstalled preparation service accepted request", err)
	}
}
