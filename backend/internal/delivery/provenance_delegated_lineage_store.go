package delivery

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/provenance"
 "photoeditor/backend/internal/v2input"
)

// SQLDelegatedLineageStore preserves the V31 immutable publication protocol
// while authorizing each SQL step from the current V30 parent lease.
type SQLDelegatedLineageStore struct {
 DB *pgxpool.Pool
 Lease ExpansionLease
 Slot Slot
 Scope SignerDelegationScope
 Ready provenance.ReadySigningExport
 Bucket string
}

// PublishDelegatedSigningLineage performs no object IO inside SQL. Completion
// rechecks parent authority after Put/Recover, including on exact retries.
func PublishDelegatedSigningLineage(ctx context.Context,store SQLDelegatedLineageStore,
 objects artifactjson.Objects,expected v2input.Ref,raw []byte)(v2input.Ref,error){
 if err:=ctx.Err();err!=nil{return v2input.Ref{},err}
 if objects==nil{return v2input.Ref{},v2input.ErrInvalid}
 body:=append([]byte(nil),raw...)
 reservation,err:=store.Reserve(ctx,expected,body)
 if err!=nil{return v2input.Ref{},err}
 if reservation.Ready!=nil{
  if reservation.Ready.ID!=expected.ArtifactID||reservation.Ready.SHA256!=expected.SHA256{
   return v2input.Ref{},objectstore.ErrIntegrity
  }
  return expected,nil
 }
 key,err:=objectstore.DocumentKey(store.Ready.Tenant,store.Ready.Run,expected.ArtifactID)
 if err!=nil{return v2input.Ref{},err}
 object,err:=objects.PutImmutable(ctx,key,bytes.NewReader(body),int64(len(body)),
  expected.SHA256,"application/json")
 if err:=ctx.Err();err!=nil{return v2input.Ref{},err}
 if errors.Is(err,objectstore.ErrExists)||errors.Is(err,objectstore.ErrUnavailable){
  object,err=objects.RecoverImmutable(ctx,key,int64(len(body)),expected.SHA256,
   "application/json")
 }
 if err!=nil{return v2input.Ref{},err}
 if object.Key!=key||object.SHA256!=expected.SHA256||
  object.Bytes!=int64(len(body))||object.ContentType!="application/json"||
  object.VersionID==""||object.VersionID=="null"{
  return v2input.Ref{},objectstore.ErrIntegrity
 }
 return store.Complete(ctx,expected,body,reservation,object)
}

func(s SQLDelegatedLineageStore)scopePins(expected v2input.Ref,raw []byte)([]byte,error){
 if s.DB==nil||len(s.Bucket)<3||len(s.Bucket)>63||
  ValidateSignerDelegationScope(s.Lease,s.Slot,s.Scope)!=nil||
  s.Ready.Tenant!=s.Scope.TenantID||s.Ready.Project!=s.Scope.ProjectID||
  s.Ready.ExportID!=s.Scope.ExportID||s.Ready.Ref.ArtifactID!=s.Scope.ExportArtifactID||
  s.Ready.Ref.SHA256!=s.Scope.ExportSHA||s.Ready.Version==uuid.Nil||
  s.Ready.Task==uuid.Nil||s.Ready.Run==uuid.Nil||
  s.Ready.Object.SHA256!=s.Ready.Ref.SHA256||
  s.Ready.Object.ContentType!="image/jpeg"&&s.Ready.Object.ContentType!="image/png"||
  s.Ready.Object.VersionID==""||s.Ready.Object.VersionID=="null"||
  s.Ready.Object.Key==""||s.Ready.Object.Bytes<1||s.Ready.Object.Bytes>512<<20||
  expected.ArtifactID==uuid.Nil||!shaPattern.MatchString(expected.SHA256)||
  expected.ArtifactID!=uuid.NewSHA1(s.Scope.ExportID,[]byte("signing:lineage:"+expected.SHA256))||
  len(raw)<2||len(raw)>8<<20||digest(raw)!=expected.SHA256{
  return nil,v2input.ErrInvalid
 }
 if _,err:=provenance.DecodeSigningLineage(raw,expected);err!=nil{return nil,err}
 return json.Marshal(map[string]any{"project":s.Ready.Project,"version":s.Ready.Version,
  "task":s.Ready.Task,"run":s.Ready.Run,"artifact":s.Ready.Ref.ArtifactID,
  "sha256":s.Ready.Ref.SHA256,"key":s.Ready.Object.Key,
  "object_version":s.Ready.Object.VersionID,"bytes":s.Ready.Object.Bytes,
  "media_type":s.Ready.Object.ContentType})
}

// VerifyReady requires the current parent lease, export pins, document row and
// exact ready artifact to agree with the canonical lineage body.
func(s SQLDelegatedLineageStore)VerifyReady(ctx context.Context,
 expected v2input.Ref,raw []byte)error{
 if err:=ctx.Err();err!=nil{return err}
 pins,err:=s.scopePins(expected,raw);if err!=nil{return err}
 var ready bool
 err=s.DB.QueryRow(ctx,`SELECT photo.v31_delivery_lineage_ready(
  $1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9)`,s.Lease.Expansion,s.Lease.Token,
  s.Slot.Ordinal,s.Slot.PresetID,string(pins),expected.ArtifactID,expected.SHA256,
  int64(len(raw)),s.Bucket).Scan(&ready)
 if err!=nil{return err}
 if !ready{return v2input.ErrInvalid}
 return ctx.Err()
}

func(s SQLDelegatedLineageStore)Reserve(ctx context.Context,expected v2input.Ref,
 raw []byte)(artifactjson.Reservation,error){
 if err:=ctx.Err();err!=nil{return artifactjson.Reservation{},err}
 pins,err:=s.scopePins(expected,raw);if err!=nil{return artifactjson.Reservation{},err}
 var outcome string;var id,token *uuid.UUID;var ready bool
 err=s.DB.QueryRow(ctx,`SELECT outcome,artifact_id,token,ready FROM photo.reserve_v31_delivery_lineage(
  $1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9)`,s.Lease.Expansion,s.Lease.Token,
  s.Slot.Ordinal,s.Slot.PresetID,string(pins),expected.ArtifactID,expected.SHA256,
  int64(len(raw)),s.Bucket).Scan(&outcome,&id,&token,&ready)
 if err!=nil{return artifactjson.Reservation{},err}
 switch outcome{
 case "quota":return artifactjson.Reservation{},artifactjson.ErrQuota
 case "conflict":return artifactjson.Reservation{},artifactjson.ErrConflict
 case "created","resumed","replayed":
 default:return artifactjson.Reservation{},artifactjson.ErrStale
 }
 if id==nil||*id!=expected.ArtifactID||token==nil||*token==uuid.Nil||
  ready!=(outcome=="replayed"){return artifactjson.Reservation{},objectstore.ErrIntegrity}
 result:=artifactjson.Reservation{ArtifactID:*id,Token:*token}
 if ready{result.Ready=&artifactjson.Ref{ID:*id,SHA256:expected.SHA256}}
 return result,nil
}

func(s SQLDelegatedLineageStore)Complete(ctx context.Context,expected v2input.Ref,
 raw []byte,reservation artifactjson.Reservation,object objectstore.Object)(v2input.Ref,error){
 if err:=ctx.Err();err!=nil{return v2input.Ref{},err}
 pins,err:=s.scopePins(expected,raw);if err!=nil{return v2input.Ref{},err}
 key,err:=objectstore.DocumentKey(s.Ready.Tenant,s.Ready.Run,expected.ArtifactID)
 if err!=nil||reservation.ArtifactID!=expected.ArtifactID||reservation.Token==uuid.Nil||
  reservation.Ready!=nil||object.Key!=key||object.SHA256!=expected.SHA256||
  object.Bytes!=int64(len(raw))||object.ContentType!="application/json"||
  object.VersionID==""||object.VersionID=="null"||len(object.VersionID)>1024{
  return v2input.Ref{},objectstore.ErrIntegrity
 }
 var accepted bool
 err=s.DB.QueryRow(ctx,`SELECT photo.complete_v31_delivery_lineage(
  $1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9,$10,$11)`,s.Lease.Expansion,
  s.Lease.Token,s.Slot.Ordinal,s.Slot.PresetID,string(pins),expected.ArtifactID,
  expected.SHA256,int64(len(raw)),s.Bucket,reservation.Token,object.VersionID).Scan(&accepted)
 if err!=nil{return v2input.Ref{},err}
 if !accepted{return v2input.Ref{},artifactjson.ErrStale}
 return expected,nil
}
