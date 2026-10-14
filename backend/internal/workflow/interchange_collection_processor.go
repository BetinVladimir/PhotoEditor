package workflow

import (
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/hex"
 "errors"
 "io"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/database"
 "photoeditor/backend/internal/objectstore"
)

type InterchangeCollectionCandidate struct {Tenant,Workflow,Artifact uuid.UUID; SHA string; Bytes int64; Bucket,Key,Version string}
type InterchangeCollectionStore interface {
 Next(context.Context)(*InterchangeCollectionCandidate,error)
 Prepare(context.Context,InterchangeCollectionCandidate,[]byte)(bool,error)
}
type interchangeCollectionObjects interface{Open(context.Context,objectstore.Object)(io.ReadCloser,error)}
type InterchangeCollectionProcessor struct {Store InterchangeCollectionStore; Objects interchangeCollectionObjects; Bucket string}
func(p InterchangeCollectionProcessor)ProcessOne(ctx context.Context)(bool,error){
 if err:=ctx.Err();err!=nil{return false,err}
 if p.Store==nil||p.Objects==nil||len(p.Bucket)<3||len(p.Bucket)>63{return false,errors.New("invalid interchange collection processor")}
 work,cancel:=context.WithTimeout(ctx,2*time.Minute);defer cancel()
 candidate,err:=p.Store.Next(work);if err!=nil||candidate==nil{return false,err}
 c:=*candidate
 if err:=work.Err();err!=nil{return false,err}
 if c.Tenant==uuid.Nil||c.Workflow==uuid.Nil||c.Artifact==uuid.Nil||!validInterchangeCollectionSHA(c.SHA)||c.Bucket!=p.Bucket||c.Bytes<2||c.Bytes>8<<20||c.Key==""||c.Version==""||c.Version=="null"{return false,errors.New("invalid interchange collection source")}
 reader,err:=p.Objects.Open(work,objectstore.Object{Key:c.Key,VersionID:c.Version,SHA256:c.SHA,ContentType:"application/json",Bytes:c.Bytes});if err!=nil{if reader!=nil{_ = reader.Close()};return false,err}
 if reader==nil{return false,errors.New("missing interchange collection reader")}
 if err:=work.Err();err!=nil{_ = reader.Close();return false,err}
 raw,readErr:=io.ReadAll(io.LimitReader(reader,c.Bytes+1));closeErr:=reader.Close()
 if err:=work.Err();err!=nil{return false,err}
 if readErr!=nil{return false,readErr};if closeErr!=nil{return false,closeErr}
 sum:=sha256.Sum256(raw)
 if int64(len(raw))!=c.Bytes||hex.EncodeToString(sum[:])!=c.SHA{return false,errors.New("interchange collection source hash mismatch")}
 normalized,err:=canonical.Bytes(raw);if err!=nil||!bytes.Equal(raw,normalized){return false,errors.New("interchange collection source noncanonical")}
 if err:=work.Err();err!=nil{return false,err}
 return p.Store.Prepare(work,c,raw)
}
func validInterchangeCollectionSHA(value string)bool{
 if len(value)!=64{return false}
 for _,c:=range value{if !(c>='0'&&c<='9'||c>='a'&&c<='f'){return false}}
 return true
}
type SQLInterchangeCollectionStore struct{DB *pgxpool.Pool}
func(r SQLInterchangeCollectionStore)Next(ctx context.Context)(*InterchangeCollectionCandidate,error){
 if err:=ctx.Err();err!=nil{return nil,err};if r.DB==nil{return nil,errors.New("missing collection scheduler database")}
 var c InterchangeCollectionCandidate
 err:=r.DB.QueryRow(ctx,`SELECT tenant_id,workflow_id,artifact_id,sha256,bytes,bucket,object_key,object_version FROM photo.v30_next_interchange_collection()`).Scan(&c.Tenant,&c.Workflow,&c.Artifact,&c.SHA,&c.Bytes,&c.Bucket,&c.Key,&c.Version)
 if errors.Is(err,pgx.ErrNoRows){return nil,nil};if err!=nil{return nil,err};return &c,nil
}
func(r SQLInterchangeCollectionStore)Prepare(ctx context.Context,c InterchangeCollectionCandidate,body []byte)(bool,error){
 if err:=ctx.Err();err!=nil{return false,err};if r.DB==nil{return false,errors.New("missing collection scheduler database")}
 var id *uuid.UUID
 err:=database.TenantTransaction(ctx,r.DB,c.Tenant,func(tx pgx.Tx)error{return tx.QueryRow(ctx,`SELECT photo.prepare_v30_interchange_draft_collection($1,$2,$3)`,c.Tenant,c.Workflow,string(body)).Scan(&id)})
 if err!=nil||id==nil{return false,err}
 if *id!=uuid.NewSHA1(c.Workflow,[]byte("interchange:draft-collection:1")){return false,errors.New("interchange collection identity mismatch")}
 return true,nil
}
