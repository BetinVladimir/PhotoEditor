package workflow
import (
 "context"
 "testing"
 "errors"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
type detectionDocumentDB struct{calls int}
func(d *detectionDocumentDB) Begin(context.Context)(pgx.Tx,error){d.calls++;return nil,errors.New("database must not be reached")}
func TestReplacementDetectionDocumentsRejectAlteredScopeAndObjectBeforeSQL(t *testing.T){
 descriptor,detector:=replacementDetectionDescriptorFixture(t)
 raw,err:=canonical.Marshal(descriptor);if err!=nil{t.Fatal(err)}
 db:=&detectionDocumentDB{}
 r:=ReplacementDetectionDocuments{DB:db,Bucket:"photos",Snapshot:uuid.New(),Plan:PerspectiveReplacementDetectionPlan{detector:detector,descriptor:string(raw)}}
 scope,body,task,_,err:=r.Plan.PreparePublication(context.Background());if err!=nil{t.Fatal(err)}
 bad:=scope;bad.Owner=uuid.New()
 if _,err=r.Reserve(context.Background(),bad,task.Params.SHA256,int64(len(body)));err!=v2input.ErrInvalid{t.Fatal("foreign owner reached SQL",err)}
 if _,err=r.Reserve(context.Background(),scope,task.Params.SHA256,int64(len(body))+1);err!=v2input.ErrInvalid{t.Fatal("wrong bytes reached SQL",err)}
 ticket:=artifactjson.Reservation{ArtifactID:task.Params.ID,Token:uuid.New()}
 key,err:=artifactjson.DocumentKey(scope,ticket.ArtifactID);if err!=nil{t.Fatal(err)}
 obj:=objectstore.Object{Key:key,VersionID:"null",SHA256:task.Params.SHA256,Bytes:int64(len(body)),ContentType:"application/json"}
 if _,err=r.Complete(context.Background(),scope,ticket,obj);err!=objectstore.ErrIntegrity{t.Fatal("unversioned object accepted",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err=r.Reserve(ctx,scope,task.Params.SHA256,int64(len(body)));err!=context.Canceled{t.Fatal("cancelled reserve accepted",err)}
 if err=r.ReserveOutput(ctx);err!=context.Canceled{t.Fatal("cancelled output reserve accepted",err)}
 if _,err=r.Publish(context.Background(),nil);err!=v2input.ErrInvalid{t.Fatal("publication without object writer accepted",err)}
 altered:=task;altered.InputDigest="wrong"
 if err=r.Stage(context.Background(),altered);err!=v2input.ErrInvalid{t.Fatal("altered task reached staging",err)}
 if err=r.Stage(ctx,task);err!=context.Canceled{t.Fatal("cancelled task staged",err)}
 if db.calls!=0{t.Fatal("invalid document reached database",db.calls)}
}
