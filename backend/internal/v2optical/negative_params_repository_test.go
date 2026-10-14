package v2optical

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "io"
 "strings"
 "testing"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/objectstore"
)

type negativeParamsDB struct{tx *negativeParamsTx;begins int}
func(d *negativeParamsDB)Begin(context.Context)(pgx.Tx,error){d.begins++;return d.tx,nil}
type negativeParamsTx struct{
 negativeDraftStoreTx
 paramsID,token uuid.UUID
 publicationMode string
 expectedDraft string
}
func(t *negativeParamsTx)QueryRow(ctx context.Context,q string,args ...any)pgx.Row{
 if !strings.Contains(q,"v29_negative_params_document"){return t.negativeDraftStoreTx.QueryRow(ctx,q,args...)}
 t.calls=append(t.calls,q)
 return negativeLockRow(func(dest ...any)error{
  var proof struct{Draft string `json:"draft_jcs"`;Objects []NegativeQualificationObject `json:"qualification_objects"`}
  if args[0]!=t.tenant||json.Unmarshal([]byte(args[len(args)-1].(string)),&proof)!=nil||proof.Draft!=t.expectedDraft||len(proof.Objects)!=2{return errors.New("publication lost exact draft/qualification proof")}
  if strings.Contains(q,"reserve_"){
   outcome:="created";if t.publicationMode=="replayed"{outcome="replayed"};if t.publicationMode=="quota"{outcome="quota"}
   *dest[0].(*string)=outcome
   id,token:=t.paramsID,t.token;if t.publicationMode=="foreign_id"{id=uuid.New()}
   *dest[1].(**uuid.UUID)=&id;*dest[2].(**uuid.UUID)=&token;*dest[3].(*bool)=outcome=="replayed"
  }else{
   if t.publicationMode=="complete_error"{return errors.New("completion dependency failure")}
   *dest[0].(*bool)=t.publicationMode!="stale_complete"
  }
  return nil
 })
}

type negativeParamsObjects struct{tx *negativeParamsTx;body []byte;mode string;puts,recoveries int}
func(o *negativeParamsObjects)PutImmutable(ctx context.Context,key string,reader io.Reader,size int64,sha,mime string)(objectstore.Object,error){
 o.puts++
 if o.tx.commits!=2{return objectstore.Object{},errors.New("object IO before durable save/reservation")}
 raw,err:=io.ReadAll(reader);if err!=nil||!bytes.Equal(raw,o.body)||size!=int64(len(raw)){return objectstore.Object{},errors.New("wrong original Params")}
 if o.mode=="recover"{return objectstore.Object{},objectstore.ErrUnavailable}
 if o.mode=="put_error"{return objectstore.Object{},errors.New("object dependency failure")}
 return objectstore.Object{Key:key,SHA256:sha,Bytes:size,ContentType:mime,VersionID:"v1"},nil
}
func(o *negativeParamsObjects)RecoverImmutable(ctx context.Context,key string,size int64,sha,mime string)(objectstore.Object,error){
 o.recoveries++
 if o.tx.commits!=2{return objectstore.Object{},errors.New("recovery inside SQL transaction")}
 return objectstore.Object{Key:key,SHA256:sha,Bytes:size,ContentType:mime,VersionID:"v1"},nil
}

func negativeParamsFixture(t *testing.T)(NegativeDraft,LocatedNegativeQualification,auth.Identity,[]byte){
 t.Helper()
 params,source,qualified,capacity:=negativeDraftFixture(t);body,_,_:=params.Compile()
 who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 draft,err:=CompileNegativeDraft(context.Background(),who,uuid.New(),uuid.New(),uuid.New(),"negative-draft-key-0001",body,source,qualified,capacity);if err!=nil{t.Fatal(err)}
 located,err:=BindNegativeQualificationObjects(qualified,"photos",
  objectstore.Object{SHA256:qualified.pin.Report.SHA256,Bytes:int64(len(qualified.reportRaw)),ContentType:"application/json",Key:"report",VersionID:"r1"},
  objectstore.Object{SHA256:qualified.pin.Calibration.SHA256,Bytes:int64(len(qualified.calibrationRaw)),ContentType:"application/json",Key:"calibration",VersionID:"c1"});if err!=nil{t.Fatal(err)}
 return draft,located,who,body
}

func TestNegativeParamsPublicationDurableReservationAndRecovery(t *testing.T){
 for _,mode:=range []string{"created","replayed","recover","quota","foreign_id","stale_complete","complete_error","put_error"}{
  t.Run(mode,func(t *testing.T){
   draft,located,who,body:=negativeParamsFixture(t);s,_:=draft.Snapshot()
   tx:=&negativeParamsTx{negativeDraftStoreTx:negativeDraftStoreTx{negativeSourceScopeTx:negativeSourceScopeTx{negativeLockTx:negativeLockTx{tenant:s.Owner.TenantID}}},paramsID:s.ParamsDocument,token:uuid.New(),publicationMode:mode,expectedDraft:draft.raw}
   db:=&negativeParamsDB{tx:tx};objects:=&negativeParamsObjects{tx:tx,body:body,mode:mode}
   ref,err:=PublishNegativeParams(context.Background(),db,objects,"photos",who,s.Owner.TenantID,draft,located)
   if mode=="created"||mode=="replayed"||mode=="recover"{
    if err!=nil||ref.ID!=s.ParamsDocument||ref.SHA256!=s.ParamsSHA{t.Fatal("valid publication refused",err)}
    if mode=="replayed"{if objects.puts!=0||objects.recoveries!=0||tx.commits!=2{t.Fatal("ready replay wrote object again")}}else if objects.puts!=1||tx.commits!=3{t.Fatal("publication phases missing")}
    if mode=="recover"&&objects.recoveries!=1{t.Fatal("ambiguous PUT was not recovered")}
   }else{
    if err==nil||ref.ID!=uuid.Nil{t.Fatal("invalid publication accepted",mode)}
    if mode=="quota"&&!errors.Is(err,artifactjson.ErrQuota){t.Fatal("quota classification lost",err)}
    if mode=="stale_complete"&&!errors.Is(err,artifactjson.ErrStale){t.Fatal("stale completion classification lost",err)}
    if (mode=="quota"||mode=="foreign_id")&&objects.puts!=0{t.Fatal("invalid reservation reached object IO")}
   }
   if tx.rollbacks!=db.begins{t.Fatal("transaction cleanup missing",tx.rollbacks,db.begins)}
  })
 }
}

func TestNegativeParamsRepositoryRejectsForeignScopeBeforeSQL(t *testing.T){
 draft,located,who,body:=negativeParamsFixture(t);s,_:=draft.Snapshot()
 for _,mode:=range []string{"tenant","run","owner","purpose","assembly","local","donor","sha","size","actor","qualification"}{
  t.Run(mode,func(t *testing.T){
   db:=&negativeParamsDB{}
   repo:=negativeParamsRepository{db:db,bucket:"photos",tenant:s.Owner.TenantID,who:who,plan:draft,located:located}
   scope:=artifactjson.Scope{Tenant:s.Owner.TenantID,Run:s.Owner.RunID,Owner:s.Owner.TaskID,Purpose:"task_params"};sha,size:=s.ParamsSHA,int64(len(body))
   switch mode{case "tenant":scope.Tenant=uuid.New();case "run":scope.Run=uuid.New();case "owner":scope.Owner=uuid.New();case "purpose":scope.Purpose="v2_input_manifest";case "assembly":scope.AssemblyLease=uuid.New();case "local":scope.LocalPortrait=true;case "donor":scope.DonorPortrait=true;case "sha":sha=strings.Repeat("f",64);case "size":size++;case "actor":repo.who.UserID=uuid.New();case "qualification":repo.located=LocatedNegativeQualification{}}
   if _,err:=repo.Reserve(context.Background(),scope,sha,size);err==nil||db.begins!=0{t.Fatal("foreign/invalid proof reached SQL",mode,err)}
  })
 }
}
