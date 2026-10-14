package workflow

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

func TestReplacementDetectionTransportRepositoryRefusesInvalidIOBeforeDB(t *testing.T){
 descriptor,detector:=replacementDetectionDescriptorFixture(t)
 raw,err:=canonical.Marshal(descriptor);if err!=nil{t.Fatal(err)}
 db:=&detectionDocumentDB{}
 source:=ReplacementDetectionDocuments{DB:db,Bucket:"photos",Snapshot:uuid.New(),Plan:PerspectiveReplacementDetectionPlan{detector:detector,descriptor:string(raw)}}
 _,_,task,entries,err:=source.Plan.PreparePublication(context.Background());if err!=nil{t.Fatal(err)}
 body,err:=canonical.Marshal(v2input.Chunk{SchemaVersion:1,Entries:entries});if err!=nil{t.Fatal(err)}
 doc,err:=CompilePerspectiveLinesTransportDocument(detector,task.Params,0,"InputClosureChunk",body,nil);if err!=nil{t.Fatal(err)}
 repository:=&replacementDetectionTransportRepository{source:source,document:doc}
 scope:=doc.Scope();wrong:=scope;wrong.Owner=uuid.New()
 if _,err=repository.Reserve(context.Background(),wrong,doc.SHA256(),int64(len(body)));err!=v2input.ErrInvalid{t.Fatal("foreign owner reached SQL",err)}
 if _,err=repository.Reserve(context.Background(),scope,doc.SHA256(),int64(len(body))+1);err!=v2input.ErrInvalid{t.Fatal("wrong size reached SQL",err)}
 ticket:=artifactjson.Reservation{ArtifactID:uuid.New(),Token:uuid.New()}
 key,err:=artifactjson.DocumentKey(scope,ticket.ArtifactID);if err!=nil{t.Fatal(err)}
 obj:=objectstore.Object{Key:key,VersionID:"null",SHA256:doc.SHA256(),Bytes:int64(len(body)),ContentType:"application/json"}
 if _,err=repository.Complete(context.Background(),scope,ticket,obj);err!=objectstore.ErrIntegrity{t.Fatal("unversioned object reached SQL",err)}
 obj.VersionID="version";obj.Key="foreign/key"
 if _,err=repository.Complete(context.Background(),scope,ticket,obj);err!=objectstore.ErrIntegrity{t.Fatal("foreign object key reached SQL",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err=repository.Reserve(ctx,scope,doc.SHA256(),int64(len(body)));err!=context.Canceled{t.Fatal("cancelled reserve reached SQL",err)}
 if _,err=source.PublishInputClosure(context.Background(),nil);err!=v2input.ErrInvalid{t.Fatal("missing object writer accepted",err)}
 if _,err=source.PublishInputClosure(ctx,nil);err!=context.Canceled{t.Fatal("cancelled publication accepted",err)}
 if db.calls!=0{t.Fatal("invalid IO reached database",db.calls)}
}
