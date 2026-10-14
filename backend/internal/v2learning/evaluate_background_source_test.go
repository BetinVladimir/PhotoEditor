package v2learning

import (
 "context"
 "errors"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

func TestEvaluateBackgroundObjectRequiresExactImmutableDescriptor(t *testing.T){
 pin:=v2draft.FrozenBackgroundAsset{SchemaVersion:1,PortableAssetID:"installed",StyleVersionID:uuid.New(),Style:v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)},Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("b",64)},ExpectedBytes:10,MediaType:"image/png",Suffix:".png",WorkICCSHA:strings.Repeat("c",64)}
 for _,mode:=range []string{"valid","source","sha","bucket","missing_bucket","key","version","null_version","bytes","media","pin"}{t.Run(mode,func(t *testing.T){
  owned:=pin;found:=pin.Source;bucket,expected:="photos","photos"
  object:=objectstore.Object{Key:"encoded/background.png",VersionID:"immutable-version",Bytes:10,ContentType:"image/png"}
  switch mode{
  case "source":found.ArtifactID=uuid.New()
  case "sha":found.SHA256=strings.Repeat("d",64)
  case "bucket":bucket="foreign"
  case "missing_bucket":expected=""
  case "key":object.Key=""
  case "version":object.VersionID=""
  case "null_version":object.VersionID="null"
  case "bytes":object.Bytes++
  case "media":object.ContentType="image/jpeg"
  case "pin":owned.Source.ArtifactID=uuid.Nil
  }
  err:=validateEvaluateBackgroundObject(owned,found,bucket,expected,object)
  if mode=="valid"{if err!=nil{t.Fatal(err)}}else if !errors.Is(err,ErrInvalidTrainingDraft){t.Fatalf("mismatched descriptor admitted: %v",err)}
 })}
}

func TestEvaluateBackgroundVerifierCancellationPrecedesMissingDependencies(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 err:=(SQLLearningEvaluateStyleSource{}).VerifyBackground(ctx,LearningEvaluateLease{},v2draft.FrozenBackgroundAsset{})
 if !errors.Is(err,context.Canceled){t.Fatalf("cancellation lost: %v",err)}
}
