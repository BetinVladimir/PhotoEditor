package v2region

import (
	"encoding/json"
	"github.com/google/uuid"
	"testing"
)

func TestAuthoredRegionRequestRetainsFullSavedDraftIdentity(t *testing.T) {
	i := authoredRegionIntentFixture()
	request := AuthoredRegionRequest{Preview: RegionPreviewRequest{VersionID: i.Binding.AssetVersionID, Rect: i.Rect, Recipe: RegionRecipeRef{i.Recipe.ArtifactID, i.Recipe.SHA256}, MaxEdge: i.MaxEdge}, DraftDocument: i.DraftDocument, DraftRevision: i.DraftRevision, FrozenDocument: i.FrozenDocument}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeAuthoredRegionRequest(raw)
	if err != nil || decoded != request || decoded.ValidateIntent(i) != nil {
		t.Fatal("authored identity lost", err)
	}
	for name, mutate := range map[string]func(*AuthoredRegionRequest){
		"revision": func(r *AuthoredRegionRequest) { r.DraftRevision++ },
		"frozen":   func(r *AuthoredRegionRequest) { r.FrozenDocument = uuid.New() },
		"source":   func(r *AuthoredRegionRequest) { r.Preview.VersionID = uuid.New() },
		"roi":      func(r *AuthoredRegionRequest) { r.Preview.Rect.X0 = .2 },
		"recipe":   func(r *AuthoredRegionRequest) { r.Preview.Recipe.ArtifactID = uuid.New() },
	} {
		t.Run(name, func(t *testing.T) {
			changed := request
			mutate(&changed)
			if changed.ValidateIntent(i) == nil {
				t.Fatal("stale or substituted immutable intent accepted")
			}
		})
	}
	var fields map[string]any
	if json.Unmarshal(raw, &fields) != nil {
		t.Fatal("fixture")
	}
	for _, key := range []string{"preview", "draft_document_id", "draft_revision", "frozen_document_id"} {
		copy := map[string]any{}
		for k, v := range fields {
			copy[k] = v
		}
		delete(copy, key)
		bad, _ := json.Marshal(copy)
		if _, err := DecodeAuthoredRegionRequest(bad); err == nil {
			t.Fatalf("missing %s accepted", key)
		}
	}
	fields["backend"] = "mps"
	bad, _ := json.Marshal(fields)
	if _, err := DecodeAuthoredRegionRequest(bad); err == nil {
		t.Fatal("caller qualification field accepted")
	}
	// The old scalar contract must continue to reject the authored envelope.
	if _, err := DecodeRegionPreviewRequest(raw); err == nil {
		t.Fatal("scalar admission widened")
	}
}

func TestAuthoredRegionRequestRejectsUnsafeWireRevision(t *testing.T){
 i:=authoredRegionIntentFixture()
 request:=AuthoredRegionRequest{Preview:RegionPreviewRequest{VersionID:i.Binding.AssetVersionID,Rect:i.Rect,Recipe:RegionRecipeRef{i.Recipe.ArtifactID,i.Recipe.SHA256},MaxEdge:i.MaxEdge},DraftDocument:i.DraftDocument,FrozenDocument:i.FrozenDocument,DraftRevision:9007199254740991}
 raw,err:=json.Marshal(request);if err!=nil{t.Fatal(err)}
 if _,err=DecodeAuthoredRegionRequest(raw);err!=nil{t.Fatal("maximum JSON-safe revision rejected",err)}
 request.DraftRevision++
 raw,err=json.Marshal(request);if err!=nil{t.Fatal(err)}
 if _,err=DecodeAuthoredRegionRequest(raw);err==nil{t.Fatal("revision rounded by a browser accepted")}
}
