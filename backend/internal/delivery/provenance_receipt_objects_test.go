package delivery

import (
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/hex"
 "io"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

type signedReceiptObjectStub struct{contents map[string][]byte;calls int}
func(s *signedReceiptObjectStub)Open(_ context.Context,o objectstore.Object)(io.ReadCloser,error){s.calls++;return io.NopCloser(bytes.NewReader(s.contents[o.Key])),nil}
func signingObjectHash(data []byte)string{sum:=sha256.Sum256(data);return hex.EncodeToString(sum[:])}

func TestSignedDependencyObjectsExactVersionBytes(t *testing.T){
 tenant,task:=uuid.New(),uuid.New();signed,manifest:=uuid.New(),uuid.New()
 signedRaw,manifestRaw:=[]byte("signed-jpeg"),[]byte("binary-c2pa")
 prefix:="tenants/"+tenant.String()+"/tasks/"+task.String()+"/attempts/1/fences/2/"
 receipt:=SignedDependencyReceipt{Signed:v2input.Ref{ArtifactID:signed,SHA256:signingObjectHash(signedRaw)},Manifest:v2input.Ref{ArtifactID:manifest,SHA256:signingObjectHash(manifestRaw)},ValidationSHA:signingObjectHash([]byte("physical-validation")),TaskID:task,ResultID:uuid.New(),Bucket:"photos"}
 receipt.SignedObject=objectstore.Object{Key:prefix+signed.String(),VersionID:"version-s",SHA256:receipt.Signed.SHA256,ContentType:"image/jpeg",Bytes:int64(len(signedRaw))}
 receipt.ManifestObject=objectstore.Object{Key:prefix+manifest.String(),VersionID:"version-m",SHA256:receipt.Manifest.SHA256,ContentType:"application/c2pa",Bytes:int64(len(manifestRaw))}
 reader:=&signedReceiptObjectStub{contents:map[string][]byte{receipt.SignedObject.Key:signedRaw,receipt.ManifestObject.Key:manifestRaw}}
 if err:=VerifySignedDependencyObjects(context.Background(),tenant,receipt,reader);err!=nil||reader.calls!=2{t.Fatal("signed objects not verified",err,reader.calls)}
 reader.calls=0;reader.contents[receipt.SignedObject.Key]=[]byte("substituted!!")
 if err:=VerifySignedDependencyObjects(context.Background(),tenant,receipt,reader);err!=objectstore.ErrIntegrity||reader.calls!=1{t.Fatal("substituted signed bytes reached manifest",err,reader.calls)}
 reader.contents[receipt.SignedObject.Key]=signedRaw;reader.calls=0
 wrong:=receipt;wrong.ManifestObject.Key="tenants/"+uuid.New().String()+"/tasks/"+task.String()+"/attempts/1/fences/2/"+manifest.String()
 if err:=VerifySignedDependencyObjects(context.Background(),tenant,wrong,reader);err==nil||reader.calls!=0{t.Fatal("foreign manifest key reached storage",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=VerifySignedDependencyObjects(ctx,tenant,receipt,reader);err!=context.Canceled||reader.calls!=0{t.Fatal("cancelled verification reached storage",err)}
}
