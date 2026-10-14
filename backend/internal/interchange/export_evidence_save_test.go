package interchange
import (
 "context"
 "testing"
 "strings"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
type exportEvidenceTx struct{accepted bool;calls int;args []any;query string}
func(s *exportEvidenceTx)QueryRow(_ context.Context,query string,args ...any)pgx.Row{
 s.calls++;s.args=args;s.query=query;return ledgerRow(func(dest ...any)error{*(dest[0].(*bool))=s.accepted;return nil})
}
func TestExportEvidenceSavePinsOriginalIdentityAndSQLReceipt(t *testing.T){
 tid,who,intent,compiled,snapshot,raw:=exportLedgerFixture(t)
 frozen,err:=FreezeExportDocuments(context.Background(),intent,compiled,snapshot,raw);if err!=nil{t.Fatal(err)}
 root:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("root"))};bundle:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("bundle"))}
 config:=SQLExportOutputCatalog{Tenant:tid,Who:who,Request:intent.Request,Key:"export-key",Bucket:"photos",Frozen:frozen}
 document:=exportAdoptionEvidence{Format:intent.Request.Format,Schema:1,Tenant:tid,Actor:who.UserID,Identity:intent.Identity,RequestSHA:intent.RequestSHA,FrozenSHA:rawSHA(frozen.Body()),Root:root,Bundle:bundle,Bucket:config.Bucket}
 body,err:=canonical.Marshal(document);if err!=nil{t.Fatal(err)}
 evidence:=PreparedExportAdoption{body:body,sha:rawSHA(body),root:root,bundle:bundle}
 tx:=&exportEvidenceTx{accepted:true}
 if err:=evidence.Save(context.Background(),tx,config);err!=nil||tx.calls!=1||len(tx.args)!=7||tx.args[6]!=evidence.sha{t.Fatal("save",err)}
 if err:=evidence.Adopt(context.Background(),tx,config);err!=nil||!strings.Contains(tx.query,"photo.adopt_v30_interchange_export("){t.Fatal("adoption dispatch",err)}
 tx.accepted=false;if err:=evidence.Adopt(context.Background(),tx,config);err==nil{t.Fatal("SQL adoption refusal accepted")}
 tx.accepted=false;if err:=evidence.Save(context.Background(),tx,config);err==nil{t.Fatal("SQL refusal accepted")}
 before:=tx.calls;config.Key="another-key"
 if err:=evidence.Save(context.Background(),tx,config);err==nil||tx.calls!=before{t.Fatal("identity mismatch reached SQL")}
 if err:=evidence.Save(context.Background(),nil,config);err==nil{t.Fatal("nil transaction")}
}
