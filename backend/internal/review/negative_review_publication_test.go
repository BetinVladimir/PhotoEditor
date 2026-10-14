package review

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
)

func TestNegativeReviewPublicationBindsCanonicalOriginalParams(t *testing.T){
 base:=ArtifactRef{ID:uuid.New(),SHA256:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
 raw,err:=canonical.Marshal(map[string]any{"schema_version":1,"base":map[string]any{"source":base}});if err!=nil{t.Fatal(err)}
 sum:=sha256.Sum256(raw);params:=ArtifactRef{ID:uuid.New(),SHA256:hex.EncodeToString(sum[:])}
 if err:=validateNegativePublicationParams(raw,params,base);err!=nil{t.Fatal(err)}
 changed:=base;changed.ID=uuid.New()
 if err:=validateNegativePublicationParams(raw,params,changed);err==nil{t.Fatal("foreign base accepted")}
 changedParams:=params;changedParams.SHA256=base.SHA256
 if err:=validateNegativePublicationParams(raw,changedParams,base);err==nil{t.Fatal("unbound Params accepted")}
 padded:=append(append([]byte(nil),raw...),'\n');sum=sha256.Sum256(padded);changedParams.SHA256=hex.EncodeToString(sum[:])
 if err:=validateNegativePublicationParams(padded,changedParams,base);err==nil{t.Fatal("noncanonical Params accepted")}
}
func TestNegativeReviewPublicationRequiresConfiguredDatabase(t *testing.T){
 if _,err:=PublishNegativeReview(context.Background(),nil,NegativeReviewPublication{});err==nil{t.Fatal("missing publication DB accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=PublishNegativeReview(ctx,nil,NegativeReviewPublication{});err!=context.Canceled{t.Fatal("cancellation lost",err)}
}
