package v2optical

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "io"
 "strings"
 "testing"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

type negativeTransportDB struct{tx *negativeTransportTx;begins int}
func(d *negativeTransportDB)Begin(context.Context)(pgx.Tx,error){d.begins++;return d.tx,nil}
type negativeTransportTx struct{
 negativeStageTx
 mode string
 ids map[string]uuid.UUID
 bodies map[string]string
 expectedDraft string
 cancel context.CancelFunc
}
func(t *negativeTransportTx)QueryRow(ctx context.Context,q string,args ...any)pgx.Row{
 if !strings.Contains(q,"v29_negative_transport_document"){return t.negativeStageTx.QueryRow(ctx,q,args...)}
 t.calls=append(t.calls,q)
 return negativeLockRow(func(dest ...any)error{
  purpose:=args[3].(string);body:=args[len(args)-1].(string)
  var proof struct{Draft string `json:"draft_jcs"`;Objects []NegativeQualificationObject `json:"qualification_objects"`}
  if args[0]!=t.tenant||json.Unmarshal([]byte(args[len(args)-2].(string)),&proof)!=nil||proof.Draft!=t.expectedDraft||len(proof.Objects)!=2{return errors.New("transport lost exact owner/qualification proof")}
  if purpose!="v2_input_chunk_0"&&purpose!="v2_input_manifest"{return errors.New("unexpected document purpose")}
  sum:=sha256.Sum256([]byte(body))
  if strings.Contains(q,"reserve_"){
   if args[4]!=hex.EncodeToString(sum[:])||args[5]!=int64(len(body)){return errors.New("transport body SHA/size mismatch")}
   t.bodies[purpose]=body
   outcome:="created";if t.mode=="replayed"{outcome="replayed"};if t.mode=="quota"{outcome="quota"}
   id,token:=t.ids[purpose],uuid.New()
   *dest[0].(*string)=outcome;*dest[1].(**uuid.UUID)=&id;*dest[2].(**uuid.UUID)=&token;*dest[3].(*bool)=outcome=="replayed"
  }else{
   if body!=t.bodies[purpose]{return errors.New("completion changed original document")}
   *dest[0].(*bool)=!(t.mode=="manifest_stale"&&purpose=="v2_input_manifest")
   if t.mode=="cancel_manifest"&&purpose=="v2_input_manifest"{t.cancel()}
  }
  return nil
 })
}

type negativeTransportObjects struct{tx *negativeTransportTx;puts,recoveries int}
func(o *negativeTransportObjects)PutImmutable(ctx context.Context,key string,reader io.Reader,size int64,sha,mime string)(objectstore.Object,error){
 o.puts++
 if o.tx.commits!=2*o.puts{return objectstore.Object{},errors.New("transport object IO before staging/reservation commit")}
 raw,err:=io.ReadAll(reader);if err!=nil||int64(len(raw))!=size{return objectstore.Object{},errors.New("transport body truncated")}
 sum:=sha256.Sum256(raw);if hex.EncodeToString(sum[:])!=sha{return objectstore.Object{},errors.New("transport body changed")}
 if o.tx.mode=="recover"{return objectstore.Object{},objectstore.ErrUnavailable}
 return objectstore.Object{Key:key,Bytes:size,SHA256:sha,ContentType:mime,VersionID:"v1"},nil
}
func(o *negativeTransportObjects)RecoverImmutable(ctx context.Context,key string,size int64,sha,mime string)(objectstore.Object,error){
 o.recoveries++
 if o.tx.commits!=2*o.puts{return objectstore.Object{},errors.New("recovery before reservation commit")}
 return objectstore.Object{Key:key,Bytes:size,SHA256:sha,ContentType:mime,VersionID:"v1"},nil
}

func TestNegativeTransportPublicationUsesStagedOwnerAndExactCompleteClosure(t *testing.T){
 for _,mode:=range []string{"created","replayed","recover","quota","manifest_stale","cancel_manifest"}{
  t.Run(mode,func(t *testing.T){
   draft,located,who,_:=negativeParamsFixture(t);s,_:=draft.Snapshot()
   tx:=&negativeTransportTx{negativeStageTx:negativeStageTx{negativeDraftStoreTx:negativeDraftStoreTx{negativeSourceScopeTx:negativeSourceScopeTx{negativeLockTx:negativeLockTx{tenant:s.Owner.TenantID}}}},mode:mode,ids:map[string]uuid.UUID{"v2_input_chunk_0":uuid.New(),"v2_input_manifest":uuid.New()},bodies:map[string]string{},expectedDraft:draft.raw}
   db:=&negativeTransportDB{tx:tx};objects:=&negativeTransportObjects{tx:tx}
   ctx,cancel:=context.WithCancel(context.Background());defer cancel();tx.cancel=cancel
   graph,err:=PublishNegativeTaskTransport(ctx,db,objects,"photos",who,s.Owner.TenantID,draft,located,v2input.Ref{ArtifactID:s.ParamsDocument,SHA256:s.ParamsSHA})
   if mode=="created"||mode=="replayed"||mode=="recover"{
    if err!=nil||graph.ID!=s.Owner.TaskID||len(graph.Built.Chunks)!=1||len(graph.Direct)!=4||graph.Open==nil{t.Fatal("complete transport not published",err)}
    var chunk v2input.Chunk;var manifest v2input.Manifest
    if json.Unmarshal([]byte(tx.bodies["v2_input_chunk_0"]),&chunk)!=nil||json.Unmarshal([]byte(tx.bodies["v2_input_manifest"]),&manifest)!=nil{t.Fatal("invalid published contracts")}
    if manifest.TaskID!=graph.ID||manifest.EntryCount!=len(chunk.Entries)||manifest.EntryCount!=graph.Built.Count||manifest.EntriesSHA!=graph.Built.SHA256||len(manifest.Chunks)!=1||manifest.Chunks[0]!=graph.Built.Chunks[0]{t.Fatal("manifest differs from complete closure")}
    cursor,err:=graph.Open(context.Background());if err!=nil{t.Fatal(err)}
    for _,expected:=range chunk.Entries{entry,ok,err:=cursor.Next(context.Background());if err!=nil||!ok||entry!=expected{t.Fatal("reopened inventory differs",err)}}
    if _,ok,err:=cursor.Next(context.Background());err!=nil||ok{t.Fatal("inventory has unexpected entries",err)}
    if mode=="replayed"{if objects.puts!=0||tx.commits!=3{t.Fatal("ready transport rewritten")}}else if objects.puts!=2||tx.commits!=5{t.Fatal("missing durable transport phases")}
    if mode=="recover"&&objects.recoveries!=2{t.Fatal("ambiguous transport writes not recovered")}
   }else{
    if err==nil||graph.ID!=uuid.Nil{t.Fatal("partial transport returned as complete",mode)}
    if mode=="quota"&&(objects.puts!=0||!errors.Is(err,artifactjson.ErrQuota)){t.Fatal("quota failure reached object IO",err)}
    if mode=="manifest_stale"&&!errors.Is(err,artifactjson.ErrStale){t.Fatal("stale manifest not refused",err)}
    if mode=="cancel_manifest"&&!errors.Is(err,context.Canceled){t.Fatal("cancellation lost",err)}
   }
   if db.begins!=tx.rollbacks{t.Fatal("transport transaction cleanup missing")}
  })
 }
}

func TestNegativeTransportRepositoryRejectsForeignPurposeAndScopeBeforeSQL(t *testing.T){
 draft,located,who,body:=negativeParamsFixture(t);s,_:=draft.Snapshot();sum:=sha256.Sum256(body)
 for _,mode:=range []string{"purpose","extra_chunk","owner","tenant","run","actor","sha","size","assembly","local","donor"}{
  t.Run(mode,func(t *testing.T){
   db:=&negativeTransportDB{}
   repo:=negativeTransportRepository{params:negativeParamsRepository{db:db,bucket:"photos",tenant:s.Owner.TenantID,who:who,plan:draft,located:located},body:string(body)}
   scope:=artifactjson.Scope{Tenant:s.Owner.TenantID,Run:s.Owner.RunID,Owner:s.Owner.TaskID,Purpose:"v2_input_chunk_0"};sha,size:=hex.EncodeToString(sum[:]),int64(len(body))
   switch mode{case "purpose":scope.Purpose="task_params";case "extra_chunk":scope.Purpose="v2_input_chunk_1";case "owner":scope.Owner=uuid.New();case "tenant":scope.Tenant=uuid.New();case "run":scope.Run=uuid.New();case "actor":repo.params.who.UserID=uuid.New();case "sha":sha=strings.Repeat("f",64);case "size":size++;case "assembly":scope.AssemblyLease=uuid.New();case "local":scope.LocalPortrait=true;case "donor":scope.DonorPortrait=true}
   if _,err:=repo.Reserve(context.Background(),scope,sha,size);err==nil||db.begins!=0{t.Fatal("invalid transport scope reached SQL",mode,err)}
  })
 }
}
