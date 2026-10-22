package v2render

import (
 "bytes"
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
 "errors"
 "encoding/json"
 "strings"
 "crypto/sha256"
 "encoding/hex"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/media"
)
func TestAuthoredFaceMediaBytesCannotBeMutatedByRoute(t *testing.T){
 proof:=AuthoredFaceMedia{raw:[]byte("private physically verified bytes"),mime:"image/jpeg",width:10,height:5}
 first:=proof.Bytes();first[0]='X';if bytes.Equal(first,proof.Bytes())||proof.Bytes()[0]!='p'{t.Fatal("route mutated physical proof")}
 w,h:=proof.Dimensions();if w!=10||h!=5||proof.ContentType()!="image/jpeg"{t.Fatal("wrong immutable metadata")}
}
func TestAuthoredFaceMediaEmptyProofCannotRecheck(t *testing.T){
 if !errors.Is((AuthoredFaceMediaService{}).Recheck(context.Background(),AuthoredFaceMedia{}),v2input.ErrInvalid){t.Fatal("manufactured zero proof accepted")}
}
func TestAuthoredFaceMediaInvalidScopeNeverReachesObjects(t *testing.T){
 // Real service rejects invalid dependency/scope before any object IO. SQL/API
 // revocation and race tests require actual composed installation, not this unit.
 _,err:=(AuthoredFaceMediaService{}).Read(context.Background(),auth.Identity{},uuid.Nil,uuid.Nil,0,"bad")
 if !errors.Is(err,v2input.ErrInvalid){t.Fatal("invalid read not refused")}
}

func TestAuthoredFaceMediaExactSQLDTOAndForeignProfile(t *testing.T){
 // All actual SQL top-level fields, including profile_hash, must decode.
 raw:=[]byte(`{"candidate_sha256":"","candidate":{},"profile_hash":"`+strings.Repeat("a",64)+`","run_id":"00000000-0000-0000-0000-000000000000","task_id":"00000000-0000-0000-0000-000000000000","result_id":"00000000-0000-0000-0000-000000000000","attempt":1,"fence":1,"image":{},"manifest":{},"derivative":{}}`)
 c,err:=decodeAuthoredFaceMediaCatalog(raw);if err!=nil||c.ProfileHash!=strings.Repeat("a",64){t.Fatal("actual SQL profile field rejected",err)}
 for _,bad:=range [][]byte{bytes.Replace(raw,[]byte(`"profile_hash":"`+strings.Repeat("a",64)+`"`),[]byte(`"profile_hash":"bad"`),1),append(bytes.Clone(raw),[]byte(` {}`)...),bytes.Replace(raw,[]byte(`"fence":1`),[]byte(`"fence":1,"unknown":true`),1)}{
  if _,err:=decodeAuthoredFaceMediaCatalog(bad);err==nil{t.Fatal("malformed SQL DTO accepted")}
 }
 // Exercise the actual manifest parser, independently from DTO presence:
 // a foreign well-formed profile is not a deployed policy hash.
 srgb:=strings.Repeat("b",64);source:=review.ArtifactRef{ID:uuid.New(),SHA256:strings.Repeat("c",64)};derivative:=review.ArtifactRef{ID:uuid.New(),SHA256:strings.Repeat("d",64)}
 wi:=review.WorkingImage{Canvas:review.Canvas{Width:100,Height:50},DType:"float32"}
 profile,err:=media.ProfileHashForPolicy("preview1600",srgb,media.CurrentRendererPolicy);if err!=nil{t.Fatal(err)}
 m:=review.RenderDerivativeManifest{Source:source,Derivative:derivative,Variant:"preview1600",ProfileHash:profile,Width:100,Height:50,MediaType:"image/jpeg",ColorProfile:"srgb",MetadataPolicy:"stripped",Alpha:false}
 for _,foreign:=range []bool{false,true}{
  if foreign{m.ProfileHash=c.ProfileHash};body,e:=canonical.Marshal(m);if e!=nil{t.Fatal(e)};h:=sha256.Sum256(body);ref:=review.ArtifactRef{ID:uuid.New(),SHA256:hex.EncodeToString(h[:])}
  _,e=review.ParseRenderDerivativeManifest(body,ref,source,derivative,wi,"preview1600",srgb)
  if (e!=nil)!=foreign{t.Fatal("manifest profile authority changed",foreign,e)}
 }
 var fields map[string]json.RawMessage;if json.Unmarshal(raw,&fields)!=nil||len(fields)!=11{t.Fatal("SQL DTO inventory drift")}
}
