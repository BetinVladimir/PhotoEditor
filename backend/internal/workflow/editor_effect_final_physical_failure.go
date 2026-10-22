package workflow

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "errors"
 "hash"
 "io"

 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
)

// Only physical reads of SQL-scoped immutable objects emit this witness.
// SQL independently rechecks the entire descriptor and current build ancestry.
type editorFinalPhysicalFailure struct{Object objectstore.Object;Code string;Cause error}
func(e *editorFinalPhysicalFailure)Error()string{return e.Code}
func(e *editorFinalPhysicalFailure)Unwrap()error{return e.Cause}
func editorFinalPhysicalError(object objectstore.Object,err error)error{
 if err==nil{return nil};code:=""
 if errors.Is(err,objectstore.ErrMissing){code="EDITOR_SOURCE_OBJECT_MISSING"}else if errors.Is(err,objectstore.ErrIntegrity){code="EDITOR_SOURCE_OBJECT_INTEGRITY"}
 if code==""{return err};return &editorFinalPhysicalFailure{Object:object,Code:code,Cause:err}
}

// Embed publication methods unchanged: output upload failures are never
// misclassified as permanent loss of an input. Only Open is intercepted.
type editorFinalPhysicalObjects struct{EditorEffectsObjects}
func(o editorFinalPhysicalObjects)Open(ctx context.Context,object objectstore.Object)(io.ReadCloser,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 reader,err:=o.EditorEffectsObjects.Open(ctx,object)
 if err!=nil{return nil,editorFinalPhysicalError(object,err)}
 if reader==nil{return nil,objectstore.ErrInvalid}
 return &editorFinalPhysicalReader{inner:reader,object:object,digest:sha256.New(),ctx:ctx},nil
}
type editorFinalPhysicalReader struct{inner io.ReadCloser;object objectstore.Object;digest hash.Hash;read int64;ctx context.Context}
func(r *editorFinalPhysicalReader)Read(buffer []byte)(int,error){
 if err:=r.ctx.Err();err!=nil{return 0,err}
 n,err:=r.inner.Read(buffer);if n>0{r.read+=int64(n);_,_=r.digest.Write(buffer[:n])}
 if r.read>r.object.Bytes{return n,editorFinalPhysicalError(r.object,objectstore.ErrIntegrity)}
 if errors.Is(err,io.EOF){
  if r.read!=r.object.Bytes||hex.EncodeToString(r.digest.Sum(nil))!=r.object.SHA256{return n,editorFinalPhysicalError(r.object,objectstore.ErrIntegrity)}
  return n,err
 }
 return n,editorFinalPhysicalError(r.object,err)
}
func(r *editorFinalPhysicalReader)Close()error{return editorFinalPhysicalError(r.object,r.inner.Close())}

func(c *EditorEffectsCoordinator)finishFinalPhysicalFailure(ctx context.Context,tenant,build uuid.UUID,err error)(bool,error){
 var failure *editorFinalPhysicalFailure
 if !errors.As(err,&failure)||ctx.Err()!=nil{return false,err}
 object:=failure.Object;var accepted bool
 dbErr:=c.DB.QueryRow(ctx,`SELECT photo.fail_editor_effect_final_source($1,$2,$3,$4,$5,$6,$7,$8,$9)`,tenant,build,c.Bucket,object.Key,object.VersionID,object.SHA256,object.Bytes,object.ContentType,failure.Code).Scan(&accepted)
 if dbErr!=nil{return false,dbErr};if !accepted{return false,err};return true,nil
}
