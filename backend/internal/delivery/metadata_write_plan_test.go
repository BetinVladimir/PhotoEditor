package delivery

import (
 "encoding/json"
 "context"
 "strings"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/v2input"
)

func TestCompileMetadataWriteUsesOnlyFrozenProjection(t *testing.T){
 for _,policy:=range []string{"copyright","approved_exif"}{t.Run(policy,func(t *testing.T){
  author,camera:="Author","Camera"
  source,_:=canonical.Marshal(MetadataDocument{Author:&author,CameraModel:&camera})
  metadata:=v2input.Ref{ArtifactID:uuid.New(),SHA256:digest(source)}
  binding:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)},CanvasHash:strings.Repeat("b",64)}
  var approved []string
  if policy=="approved_exif"{approved=[]string{"camera_model"}}
  frozen,pin,err:=FreezeDeliveryMetadata(binding,metadata,source,policy,approved)
  if err!=nil{t.Fatal(err)}
  image:=metadataEncodedImage{Artifact:v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("c",64)},Format:"jpeg",Width:4,Height:3,ICCID:"srgb_v1",ICCSHA:strings.Repeat("d",64),Policy:"strip"}
  imageRaw,_:=canonical.Marshal(image)
  encoded:=v2input.Ref{ArtifactID:uuid.New(),SHA256:digest(imageRaw)}
  childRun:=uuid.New()
  workflow,_:=v2input.WorkflowID(childRun)
  task,_:=v2input.TaskID(workflow,"metadata.write",0)
  plan,err:=CompileMetadataWrite(task,encoded,imageRaw,frozen,pin,binding,metadata,source,policy,approved)
  if err!=nil{t.Fatal(err)}
  var projection MetadataDocument
  if json.Unmarshal(plan.ProjectionJCS(),&projection)!=nil{t.Fatal("projection")}
  if policy=="copyright"&&(projection.Author==nil||projection.CameraModel!=nil){t.Fatal("copyright projection")}
  if policy=="approved_exif"&&(projection.Author!=nil||projection.CameraModel==nil){t.Fatal("approved projection")}
  var params MetadataWriteParams
  if json.Unmarshal(plan.ParamsJCS(),&params)!=nil||params.Document!=plan.Projection()||params.Encoded!=encoded||params.ToolID!="exiftool_v1"||params.CopyGPS{t.Fatal("worker params")}
  if len(plan.Inputs())!=3||plan.Inputs()[1]!=image.Artifact{t.Fatal("worker inputs")}
  owned:=plan.ParamsJCS();owned[0]='!';inputs:=plan.Inputs();inputs[0]=v2input.Ref{}
  again,err:=CompileMetadataWrite(task,encoded,imageRaw,frozen,pin,binding,metadata,source,policy,approved)
  if err!=nil||again.Params()!=plan.Params()||plan.ParamsJCS()[0]=='!'||plan.Inputs()[0]!=encoded{t.Fatal("ownership/replay")}
  _,proofRaw,proofSHA,err:=CompileMetadataWriteProof(task,encoded,imageRaw,frozen,pin,binding,metadata,source,policy,approved)
  if err!=nil{t.Fatal(err)}
  scope:=MetadataProofScope{Tenant:uuid.New(),Expansion:uuid.New(),Token:uuid.New(),ChildRun:childRun,Task:task,Encoded:encoded,Metadata:metadata,Binding:binding,Policy:policy,ApprovedFields:approved}
  graph,err:=plan.PublishInputs(context.Background(),childRun,bindingPublisher{})
  if err!=nil{t.Fatal("metadata transport",err)}
  compiler:=&metadataTransportCompiler{task:task,documents:map[string]metadataTransportBody{}}
  deterministicGraph,err:=plan.PublishInputs(context.Background(),childRun,compiler)
  if err!=nil||len(compiler.documents)!=2{t.Fatal("transport compilation",err)}
  if deterministicGraph.Built.Manifest.ArtifactID!=uuid.NewSHA1(task,[]byte("delivery:metadata:transport:v2_input_manifest:1")){t.Fatal("unstable manifest identity")}
  compiledRepository:=MetadataDocumentRepository{DB:&metadataStorageDB{},Scope:scope,Plan:plan,Bucket:"photos",transport:compiler.documents}
  for purpose,document:=range compiler.documents{
   ref,body,err:=compiledRepository.document(metadataPublicationScope(scope,purpose))
   if err!=nil||ref!=document.ref||digest(body)!=ref.SHA256{t.Fatal("transport repository body",err)}
   body[0]='!'
   _,again,err:=compiledRepository.document(metadataPublicationScope(scope,purpose))
   if err!=nil||again[0]=='!'{t.Fatal("borrowed transport bytes")}
  }
  if _,_,err=compiledRepository.document(metadataPublicationScope(scope,"v2_input_chunk_1"));err==nil{t.Fatal("extra metadata chunk allowed")}
  graph.Open=func(context.Context)(v2input.Cursor,error){t.Fatal("untrusted cursor called");return nil,v2input.ErrInvalid}
  closure,err:=plan.InputClosure(childRun)
  if err!=nil{t.Fatal(err)}
  draft:=v2input.DraftTask{ID:task,NodeKey:"metadata.write:0",Operation:"metadata.write",OperationVersion:"1",ResourceClass:"cpu",ParamsArtifactID:plan.Params().ArtifactID,ParamsSHA:plan.Params().SHA256,InputDigest:closure.InputDigest,ExecutionProfile:json.RawMessage(`{"backend":"cpu"}`)}
  registration:=&metadataRegisteredTx{}
  if err=plan.RegisterInputsInTransaction(context.Background(),registration,scope,draft,graph,100);err!=nil||registration.calls!=1{t.Fatal("metadata activated replay",err,registration.calls)}
  changedGraph:=graph;changedGraph.Built.SHA256=strings.Repeat("f",64)
  if err=plan.RegisterInputsInTransaction(context.Background(),registration,scope,draft,changedGraph,100);err==nil||registration.calls!=1{t.Fatal("substituted transport reached SQL")}
  storageDB:=&metadataStorageDB{plan:plan,token:uuid.New()}
  objects:=&metadataStorageObjects{}
  repository:=MetadataDocumentRepository{DB:storageDB,Scope:scope,Plan:plan,Bucket:"photos"}
  if err=PublishMetadataDocuments(context.Background(),repository,objects);err!=nil||storageDB.calls!=4||objects.puts!=2{t.Fatal("metadata publish",err)}
  storageDB.ready=true;beforeCalls:=storageDB.calls
  if err=PublishMetadataDocuments(context.Background(),repository,objects);err!=nil||storageDB.calls-beforeCalls!=2||objects.puts!=2{t.Fatal("ready replay",err)}
  foreignScope:=artifactjson.Scope{Tenant:scope.Tenant,Run:scope.ChildRun,Owner:uuid.New(),AssemblyLease:scope.Token,Purpose:"v30_metadata_projection"}
  beforeCalls=storageDB.calls
  if _,err=repository.Reserve(context.Background(),foreignScope,plan.Projection().SHA256,int64(len(plan.ProjectionJCS())));err==nil||storageDB.calls!=beforeCalls{t.Fatal("foreign publication scope")}
  readDB:=&metadataProofReadDB{body:string(proofRaw),pin:proofSHA}
  reader:=SQLMetadataProofStore{DB:readDB}
  loaded,err:=reader.Load(context.Background(),scope)
  if err!=nil||loaded.Params()!=plan.Params(){t.Fatal("loaded proof",err)}
  readDB.pin=strings.Repeat("0",64)
  if _,err=reader.Load(context.Background(),scope);err==nil{t.Fatal("corrupt saved proof")}
  readDB.pin=proofSHA;readDB.err=pgx.ErrNoRows
  if _,err=reader.Load(context.Background(),scope);err==nil{t.Fatal("missing proof accepted")}
  readDB.err=nil
  readContext,readCancel:=context.WithCancel(context.Background());readDB.cancel=readCancel
  if _,err=reader.Load(readContext,scope);err!=context.Canceled{t.Fatal("late read cancellation",err)}
  db:=&metadataProofPinDB{accepted:true}
  store:=SQLMetadataProofStore{DB:db}
  if err=store.Pin(context.Background(),scope,proofRaw,proofSHA);err!=nil||db.calls!=1||db.body!=string(proofRaw){t.Fatal("pin",err)}
  db.accepted=false
  if err=store.Pin(context.Background(),scope,proofRaw,proofSHA);err==nil{t.Fatal("stale pin accepted")}
  cancelled,cancel:=context.WithCancel(context.Background());cancel()
  before:=db.calls
  if err=store.Pin(cancelled,scope,proofRaw,proofSHA);err!=context.Canceled||db.calls!=before{t.Fatal("cancelled pin reached SQL")}
  active,lateCancel:=context.WithCancel(context.Background());db.accepted=true;db.cancel=lateCancel
  if err=store.Pin(active,scope,proofRaw,proofSHA);err!=context.Canceled{t.Fatal("late cancellation",err)}
  db.cancel=nil
  invalidScope:=scope;invalidScope.ChildRun=uuid.New();before=db.calls
  if err=store.Pin(context.Background(),invalidScope,proofRaw,proofSHA);err==nil||db.calls!=before{t.Fatal("foreign child reached SQL")}
  restored,err:=RestoreMetadataWriteProof(proofRaw,proofSHA,task,encoded,binding,metadata,policy,approved)
  if err!=nil||restored.Params()!=plan.Params(){t.Fatal("proof replay",err)}
  if _,err=RestoreMetadataWriteProof(proofRaw,proofSHA,uuid.New(),encoded,binding,metadata,policy,approved);err==nil{t.Fatal("foreign proof task")}
  if _,err=RestoreMetadataWriteProof(proofRaw,proofSHA,task,encoded,binding,metadata,policy,[]string{"gps"});err==nil{t.Fatal("proof approval substitution")}
  var forged MetadataWriteProof
  if json.Unmarshal(proofRaw,&forged)!=nil{t.Fatal("proof fixture")}
  forged.ParamsJCS=`{"untrusted":true}`
  forgedRaw,_:=canonical.Marshal(forged)
  if _,err=RestoreMetadataWriteProof(forgedRaw,digest(forgedRaw),task,encoded,binding,metadata,policy,approved);err==nil{t.Fatal("self-consistent changed Params proof")}
  wrong:=binding;wrong.AssetVersionID=uuid.New()
  if _,err=CompileMetadataWrite(task,encoded,imageRaw,frozen,pin,wrong,metadata,source,policy,approved);err==nil{t.Fatal("foreign version")}
  if _,err=CompileMetadataWrite(task,encoded,imageRaw,frozen,pin,binding,metadata,source,policy,[]string{"gps"});err==nil{t.Fatal("changed approval")}
 })}
}


type metadataRegisteredTx struct{pgx.Tx;calls int}
func(tx *metadataRegisteredTx)QueryRow(_ context.Context,query string,args ...any)pgx.Row{
 tx.calls++
 if query!=`SELECT photo.v30_metadata_task_activated($1,$2,$3,$4)`{panic("unexpected registration on activated replay")}
 return proofRow{accepted:true}
}
