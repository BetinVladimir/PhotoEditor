package delivery

import (
	"context"
 "bytes"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
	"encoding/json"
	"github.com/google/uuid"
	"testing"
)

func TestCreatePendingRoundTripAndOwnership(t *testing.T) {
	intent, session, catalog, items := createDocumentsFixture(t)
	raw, err := intent.EncodePending(context.Background(), session, catalog, items)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := intent.DecodePending(context.Background(), session, raw)
	if err != nil {
		t.Fatal(err)
	}
	if pending.RequestSHA != intent.RequestSHA() || pending.Catalog.Snapshot != catalog.Snapshot || pending.Items[0].Recipe != items[0].Recipe {
		t.Fatal("snapshot changed")
	}
	// A new live session may recover the same pinned graph; SQL reauthorizes it.
	if _, err := intent.DecodePending(context.Background(), uuid.New(), raw); err != nil {
		t.Fatal(err)
	}
	catalog.Presets[0].ConfigJCS[0] = 'x'
	items[0].Recipe.ArtifactID = uuid.New()
	raw[0] = 'x'
	if !json.Valid(pending.Catalog.Presets[0].ConfigJCS) || pending.Items[0].Recipe == items[0].Recipe {
		t.Fatal("borrowed pending inputs")
	}
}
func TestCreatePendingRejectsScopeHashSchemaAndSourceCorruption(t *testing.T) {
	intent, session, catalog, items := createDocumentsFixture(t)
	raw, err := intent.EncodePending(context.Background(), session, catalog, items)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"tenant_id", "actor_id", "request_sha", "schema_version", "unknown"} {
		var wire map[string]any
		_ = json.Unmarshal(raw, &wire)
		if field == "schema_version" {
			wire[field] = 2
		} else {
			wire[field] = uuid.New().String()
		}
		changed, _ := json.Marshal(wire)
		if _, err := intent.DecodePending(context.Background(), session, changed); err == nil {
			t.Fatal("corruption accepted", field)
		}
	}
	var pending CreatePending
	_ = json.Unmarshal(raw, &pending)
	pending.Items[0].Recipe.ArtifactID = uuid.Nil
	broken, _ := json.Marshal(pending)
	if _, err := intent.DecodePending(context.Background(), session, broken); err == nil {
		t.Fatal("invalid recipe accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := intent.DecodePending(ctx, session, raw); err == nil {
		t.Fatal("cancel ignored")
	}
}

func TestCreatePendingRejectsDocumentSubstitution(t *testing.T) {
	intent, session, catalog, items := createDocumentsFixture(t)
	raw, err := intent.EncodePending(context.Background(), session, catalog, items)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*CreatePending){
		func(p *CreatePending) { p.Project = uuid.New() },
		func(p *CreatePending) { p.Documents[0].Body = "{}" },
		func(p *CreatePending) { p.Documents[0].Ref.ArtifactID = uuid.New() },
		func(p *CreatePending) { p.Documents[0].Purpose = "task_params" },
		func(p *CreatePending) { p.Documents = p.Documents[:len(p.Documents)-1] },
	} {
		var pending CreatePending
		if err := json.Unmarshal(raw, &pending); err != nil {
			t.Fatal(err)
		}
		mutate(&pending)
		changed, _ := json.Marshal(pending)
		if _, err := intent.DecodePending(context.Background(), session, changed); err == nil {
			t.Fatal("document substitution accepted")
		}
	}
}

func TestPendingDeliveryRetainsPresetPolicyAfterMutableCatalogChange(t *testing.T){
 for _,mode:=range []string{"strip","copyright","approved_exif"}{t.Run(mode,func(t *testing.T){
  base,session,catalog,items:=createDocumentsFixture(t)
  request:=base.Request();request.Policy.MetadataPolicy=mode
  if mode=="approved_exif"{request.Policy.ApprovedFields=[]string{"copyright","gps"}}
  intent,err:=FreezeCreateIntent(base.tenant,auth.Identity{UserID:base.actor,SessionID:session},request,"preset-policy-freeze");if err!=nil{t.Fatal(err)}
  var config map[string]any
  if err=json.Unmarshal(catalog.Presets[0].ConfigJCS,&config);err!=nil{t.Fatal(err)}
  config["metadata_policy"]=mode
  frozenConfig,err:=canonical.Marshal(config);if err!=nil{t.Fatal(err)}
  catalog.Presets[0].ConfigJCS=frozenConfig;catalog.Presets[0].SHA256=digest(frozenConfig)
  raw,err:=intent.EncodePending(context.Background(),session,catalog,items);if err!=nil{t.Fatal(err)}
  // A later mutable preset edit changes policy and hash at the same preset ID.
  nextMode:="copyright";if mode==nextMode{nextMode="strip"}
  config["metadata_policy"]=nextMode;changed,err:=canonical.Marshal(config);if err!=nil{t.Fatal(err)}
  catalog.Presets[0].ConfigJCS=changed;catalog.Presets[0].SHA256=digest(changed);catalog.Presets[0].Revision="2"
  pending,err:=intent.DecodePending(context.Background(),session,raw);if err!=nil{t.Fatal(err)}
  if !bytes.Equal(pending.Catalog.Presets[0].ConfigJCS,frozenConfig)||pending.Catalog.Presets[0].SHA256!=digest(frozenConfig){t.Fatal("pending followed mutable preset")}
  docs,err:=intent.CompileDocuments(context.Background(),session,pending.Catalog,pending.Items);if err!=nil{t.Fatal(err)}
  if !bytes.Equal(docs.Presets[0].Body,frozenConfig){t.Fatal("publication config changed")}
  rotatedSession:=uuid.New()
  rotatedPending,err:=intent.DecodePending(context.Background(),rotatedSession,raw);if err!=nil{t.Fatal(err)}
  rotatedDocs,err:=intent.CompileDocuments(context.Background(),rotatedSession,rotatedPending.Catalog,rotatedPending.Items);if err!=nil{t.Fatal(err)}
  if !bytes.Equal(docs.Frozen.Body,rotatedDocs.Frozen.Body)||!bytes.Equal(docs.Policy.Body,rotatedDocs.Policy.Body)||!bytes.Equal(docs.Params.Body,rotatedDocs.Params.Body){t.Fatal("session rotation changed frozen documents")}
  var oldProof,newProof map[string]json.RawMessage
  json.Unmarshal(docs.Plan.ProofJCS(),&oldProof);json.Unmarshal(rotatedDocs.Plan.ProofJCS(),&newProof)
  expectedSession,_:=json.Marshal(rotatedSession)
  if !bytes.Equal(newProof["auth_session_id"],expectedSession){t.Fatal("proof did not adopt current session")}
  delete(oldProof,"auth_session_id");delete(newProof,"auth_session_id")
  oldRaw,_:=canonical.Marshal(oldProof);newRaw,_:=canonical.Marshal(newProof)
  if !bytes.Equal(oldRaw,newRaw){t.Fatal("session recovery changed non-session proof")}

  if _,err=intent.CompileDocuments(context.Background(),session,catalog,items);err==nil{t.Fatal("changed metadata mode accepted under old frozen intent")}
 })}
}
