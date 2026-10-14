package delivery

import (
	"context"
	"errors"
 "encoding/json"
 "strings"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"testing"
)

type deliveryReadRow struct {
	raw []byte
	err error
}

func (r deliveryReadRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*dest[0].(*[]byte) = r.raw
	return nil
}

type deliveryReadTx struct {
	raw   []byte
	err   error
	args  []any
	calls int
}

func deliveryDataFixture(tenant, id uuid.UUID, revision, state string, manifest any, published any) map[string]any {
 return map[string]any{
  "resource_type":"delivery_packages", "id":id, "tenant_id":tenant, "project_id":uuid.New(),
  "created_at":"2026-10-11T11:00:00Z", "revision":revision, "updated_at":"2026-10-11T12:00:00Z",
  "workflow_id":uuid.New(), "snapshot_id":uuid.New(), "name":"Delivery package",
  "policy_artifact_id":uuid.New(), "manifest_artifact_id":manifest, "published_at":published, "state":state,
 }
}

func (tx *deliveryReadTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	tx.calls++
	tx.args = args
	return deliveryReadRow{tx.raw, tx.err}
}
func TestDeliveryReadBindsIdentityAndOwnsBytes(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tid, id := uuid.New(), uuid.New()
	fixture,_:=json.Marshal(map[string]any{"id":id,"revision":"1","document":nil,"data":deliveryDataFixture(tid,id,"1","building",nil,nil)})
	tx := &deliveryReadTx{raw: fixture}
	raw, err := ReadPackage(context.Background(), tx, who, tid, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(tx.args) != 4 || tx.args[0] != tid || tx.args[1] != who.UserID || tx.args[2] != who.SessionID || tx.args[3] != id {
		t.Fatal("identity binding")
	}
	tx.raw[0] = 'x'
	if raw[0] != '{' {
		t.Fatal("borrowed bytes")
	}
	if _, err := ReadPackage(context.Background(), tx, who, uuid.Nil, id); err == nil || tx.calls != 1 {
		t.Fatal("invalid scope queried")
	}
	for _, value := range [][]byte{nil, []byte("null"), []byte("broken")} {
		tx.raw = value
		if _, err := ReadPackage(context.Background(), tx, who, tid, id); err == nil {
			t.Fatal("invalid/notfound accepted")
		}
	}
	expected := errors.New("db failure")
	tx.err = expected
	if _, err := ReadPackage(context.Background(), tx, who, tid, id); !errors.Is(err, expected) {
		t.Fatal("DB failure hidden")
	}
}

func TestDeliveryReadRejectsForeignOrMalformedResourceEnvelope(t *testing.T){
 tid,id:=uuid.New(),uuid.New()
 valid:=map[string]any{"id":id.String(),"revision":"1","document":nil,"data":deliveryDataFixture(tid,id,"1","building",nil,nil)}
 for _,mode:=range []string{"foreign-id","foreign-tenant","revision","unsafe-revision","kind","extra-root","extra-data","missing-data","missing-project","invalid-created","invalid-name","wrong-manifest","unknown-state","review-without-manifest","published-building","invalid-publication-time"}{t.Run(mode,func(t *testing.T){
  raw,_:=json.Marshal(valid);var resource map[string]any;json.Unmarshal(raw,&resource);data:=resource["data"].(map[string]any)
  switch mode{case "unknown-state":data["state"]="internal";case "review-without-manifest":data["state"]="review";case "published-building":data["published_at"]="2026-10-11T12:00:00Z";case "invalid-publication-time":data["published_at"]="not-a-date";case "foreign-id":resource["id"]=uuid.NewString();case "foreign-tenant":data["tenant_id"]=uuid.NewString();case "revision":data["revision"]="2";case "unsafe-revision":resource["revision"]="9007199254740992";data["revision"]="9007199254740992";case "kind":data["resource_type"]="projects";case "extra-root":resource["bucket"]="private";case "extra-data":data["object_key"]="private";case "missing-data":delete(data,"snapshot_id");case "missing-project":data["project_id"]=uuid.Nil;case "invalid-created":data["created_at"]="invalid";case "invalid-name":data["name"]="";case "wrong-manifest":resource["document"]=map[string]any{"artifact_id":uuid.NewString(),"sha256":rawSHA([]byte("manifest"))}}
  raw,_=json.Marshal(resource)
  if ValidPackageResource(raw,tid,id){t.Fatal("invalid delivery envelope accepted")}
 })}
 raw,_:=json.Marshal(valid);if !ValidPackageResource(raw,tid,id){t.Fatal("valid metadata rejected")}
 for _,duplicate:=range []string{
  strings.Replace(string(raw),`"document":null,`,`"document":null,"document":null,`,1),
  strings.Replace(string(raw),`"data":{`,`"data":{"id":"`+id.String()+`",`,1),
 }{
  if duplicate==string(raw)||ValidPackageResource([]byte(duplicate),tid,id){t.Fatal("duplicate resource key accepted")}
 }
 manifest:=uuid.New()
 ready:=map[string]any{"id":id,"revision":"2","document":map[string]any{"artifact_id":manifest,"sha256":rawSHA([]byte("manifest"))},"data":deliveryDataFixture(tid,id,"2","ready",manifest,nil)}
 raw,_=json.Marshal(ready)
 if !ValidPackageResource(raw,tid,id){t.Fatal("valid ready metadata rejected")}
 ready["document"]=nil
 missingDocument,_:=json.Marshal(ready)
 if !ValidPackageResource(missingDocument,tid,id){t.Fatal("ready metadata must remain readable after document expiry")}
 ready["document"]=map[string]any{"artifact_id":manifest,"sha256":rawSHA([]byte("manifest"))}
 duplicate:=strings.Replace(string(raw),`"document":{`,`"document":{"artifact_id":"`+manifest.String()+`",`,1)
 if duplicate==string(raw)||ValidPackageResource([]byte(duplicate),tid,id){t.Fatal("duplicate document key accepted")}
}
