package delivery

import (
 "context"
 "io"
 "strings"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/objectstore"
)
type metadataStorageRow struct {scan func([]any)error}
func(r metadataStorageRow)Scan(dest ...any)error{return r.scan(dest)}
type metadataStorageDB struct {plan MetadataWritePlan;token uuid.UUID;ready bool;calls int}
func(d *metadataStorageDB)QueryRow(_ context.Context,q string,args ...any)pgx.Row{
 d.calls++
 purpose:=args[3].(string)
 ref,body:=d.plan.Projection(),d.plan.ProjectionJCS()
 if purpose=="task_params"{ref,body=d.plan.Params(),d.plan.ParamsJCS()}
 if args[4].(string)!=string(body){panic("wrong frozen publication bytes")}
 if strings.Contains(q,"photo.reserve_v30_metadata_document("){
  return metadataStorageRow{func(dest []any)error{
   outcome:="created";if d.ready{outcome="replayed"}
   *dest[0].(*string)=outcome;id,token:=ref.ArtifactID,d.token
   *dest[1].(**uuid.UUID)=&id;*dest[2].(**uuid.UUID)=&token;*dest[3].(*bool)=d.ready
   return nil
  }}
 }
 if !strings.Contains(q,"photo.complete_v30_metadata_document("){panic("wrong metadata completion authority")}
 return metadataStorageRow{func(dest []any)error{*dest[0].(*bool)=true;return nil}}
}
type metadataStorageObjects struct {puts int}
func(o *metadataStorageObjects)PutImmutable(_ context.Context,key string,reader io.Reader,size int64,sha,mime string)(objectstore.Object,error){
 body,err:=io.ReadAll(reader)
 if err!=nil{return objectstore.Object{},err}
 if int64(len(body))!=size||digest(body)!=sha{return objectstore.Object{},objectstore.ErrIntegrity}
 o.puts++
 return objectstore.Object{Key:key,SHA256:sha,Bytes:size,ContentType:mime,VersionID:"immutable-v1"},nil
}
func(o *metadataStorageObjects)RecoverImmutable(context.Context,string,int64,string,string)(objectstore.Object,error){return objectstore.Object{},objectstore.ErrUnavailable}
