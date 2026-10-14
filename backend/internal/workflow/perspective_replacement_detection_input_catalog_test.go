package workflow
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestReplacementDetectorInputCatalogRejectsForeignSourceBeforeSQL(t *testing.T){
 d,detector:=replacementDetectionDescriptorFixture(t)
 raw,err:=canonical.Marshal(d);if err!=nil{t.Fatal(err)}
 db:=&detectionDocumentDB{}
 r:=ReplacementDetectionDocuments{DB:db,Bucket:"photos",Snapshot:uuid.New(),Plan:PerspectiveReplacementDetectionPlan{detector:detector,descriptor:string(raw)}}
 c:=replacementDetectionInputCatalog{source:r};who:=auth.Identity{UserID:d.Actor,SessionID:d.Session}
 wrong:=detector.scope;wrong.Run=uuid.New()
 if _,err=c.ResolveSource(context.Background(),wrong,r.Snapshot,who);err!=v2input.ErrInvalid{t.Fatal("foreign run reached SQL",err)}
 if _,err=c.ResolveSource(context.Background(),detector.scope,uuid.New(),who);err!=v2input.ErrInvalid{t.Fatal("foreign snapshot reached SQL",err)}
 who.UserID=uuid.New()
 if _,err=c.ResolveSource(context.Background(),detector.scope,r.Snapshot,who);err!=v2input.ErrInvalid{t.Fatal("foreign actor reached SQL",err)}
 if _,err=r.VerifyInputObjects(context.Background(),nil);err!=v2input.ErrInvalid{t.Fatal("missing reader accepted",err)}
 if db.calls!=0{t.Fatal("invalid scope reached database")}
}
