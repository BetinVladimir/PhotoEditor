package provenance

import (
 "bytes"
 "context"
 "encoding/binary"
 "encoding/json"

 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
 "io"
 "math"
 "testing"

 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
)

type signingNegativeSourceCatalogFunc func(context.Context,auth.Identity,ReadySigningExport,CanonicalSigningTask,SigningNegativePixelEvidence,string)(objectstore.Object,error)
func(f signingNegativeSourceCatalogFunc)LookupSigningNegativeSourceArray(c context.Context,a auth.Identity,r ReadySigningExport,t CanonicalSigningTask,e SigningNegativePixelEvidence,role string)(objectstore.Object,error){return f(c,a,r,t,e,role)}

func TestNegativeSourceArraysPhysicalVerification(t *testing.T){
 for _,mode:=range []string{"valid_hdr_alpha","valid_rgb_only","valid_inverse","inverse_wrong_shape","inverse_nan","alpha_channel_axis","alpha_out_of_range","nan_rgb","version_changed","cancelled","wrong_task","missing_catalog","missing_objects"}{
  t.Run(mode,func(t *testing.T){
   npy:=func(shape string,values ...float32)[]byte{
    header:="{'descr': '<f4', 'fortran_order': False, 'shape': "+shape+", }\n"
    raw:=[]byte{0x93,'N','U','M','P','Y',1,0,0,0};binary.LittleEndian.PutUint16(raw[8:],uint16(len(header)));raw=append(raw,[]byte(header)...)
    for _,v:=range values{var encoded [4]byte;binary.LittleEndian.PutUint32(encoded[:],math.Float32bits(v));raw=append(raw,encoded[:]...)};return raw
   }
   rgb:=npy("(1, 1, 3)",2,.5,.1);if mode=="nan_rgb"{rgb=npy("(1, 1, 3)",2,.5,float32(math.NaN()))}
   alpha:=npy("(1, 1)",.5);if mode=="alpha_channel_axis"{alpha=npy("(1, 1, 1)",.5)};if mode=="alpha_out_of_range"{alpha=npy("(1, 1)",1.1)}
   task:=CanonicalSigningTask{Operation:"optical.negative",Version:"1"};digest,err:=canonical.Hash(task);if err!=nil{t.Fatal(err)}
   evidence:=SigningNegativePixelEvidence{taskDigest:digest,source:review.WorkingImage{Canvas:review.Canvas{Width:1,Height:1},Pixels:review.ArtifactRef{SHA256:bodyDigest(rgb)},Alpha:&review.ArtifactRef{SHA256:bodyDigest(alpha)}}}
   if mode=="valid_rgb_only"{evidence.source.Alpha=nil};if mode=="wrong_task"{task.Version="2"}
   bodies:=map[string][]byte{"pixels":rgb,"alpha":alpha}
   if mode=="valid_inverse"||mode=="inverse_wrong_shape"||mode=="inverse_nan"{
    field:=npy("(1, 1, 2)",-.5,2)
    if mode=="inverse_wrong_shape"{field=npy("(1, 2)",-.5,2)}
    if mode=="inverse_nan"{field=npy("(1, 1, 2)",float32(math.NaN()),2)}
    pin:=v2input.Ref{ArtifactID:uuid.New(),SHA256:bodyDigest(field)}
    evidence.source.TransformChain,_=json.Marshal(map[string]any{"steps":[]any{map[string]any{"kind":"dense_warp","to_canvas":map[string]int{"width":1,"height":1},"inverse_field":pin}}})
    bodies["InverseField/0"]=field
   }
   calls:=map[string]int{};opens:=0
   catalog:=signingNegativeSourceCatalogFunc(func(_ context.Context,_ auth.Identity,_ ReadySigningExport,_ CanonicalSigningTask,_ SigningNegativePixelEvidence,role string)(objectstore.Object,error){
    calls[role]++;raw:=bodies[role];version:="v1";if mode=="version_changed"&&calls[role]>1{version="v2"}
    return objectstore.Object{Key:role,VersionID:version,SHA256:bodyDigest(raw),Bytes:int64(len(raw)),ContentType:"application/x-npy"},nil
   })
   objects:=signingDocumentObjectsFunc(func(_ context.Context,o objectstore.Object)(io.ReadCloser,error){opens++;return io.NopCloser(bytes.NewReader(bodies[o.Key])),nil})
   verifier:=BoundSigningNegativeSourceArraysVerifier{Catalog:catalog,Objects:objects};if mode=="missing_catalog"{verifier.Catalog=nil};if mode=="missing_objects"{verifier.Objects=nil}
   ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
   err=verifier.Verify(ctx,auth.Identity{},ReadySigningExport{},task,evidence)
   valid:=mode=="valid_hdr_alpha"||mode=="valid_rgb_only"||mode=="alpha_channel_axis"||mode=="valid_inverse"
   if valid{expected:=2;if evidence.source.Alpha==nil{expected=1};if mode=="valid_inverse"{expected=3};if err!=nil||opens!=expected||calls["pixels"]!=2{t.Fatal("valid source refused",err,calls,opens)};if evidence.source.Alpha!=nil&&calls["alpha"]!=2{t.Fatal("alpha was not rechecked")};return}
   if err==nil{t.Fatal("invalid source accepted",mode)}
   if (mode=="cancelled"||mode=="wrong_task"||mode=="missing_catalog"||mode=="missing_objects")&&opens!=0{t.Fatal("invalid preflight opened objects")}
  })
 }
}
