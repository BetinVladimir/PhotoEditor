package semanticproposal

import (
 "context"
 "errors"
 "testing"
 "photoeditor/backend/internal/auth"
)

func TestSceneStagePreparationRequiresCatalogBeforeQualificationIO(t *testing.T){
 reader:=SQLTransferSourceReader{}
 if _,err:=reader.prepareSemanticSceneStageTask(context.Background(),auth.Identity{},Request{},semanticSceneStageParams{},nil,TransferSceneProducer{},nativePreparationBudget{});err==nil{t.Fatal("missing catalog accepted")}
}

func TestSceneStagePublicationCancellationBeforeQualificationIO(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 // The anchor verifier owns the first cancellation boundary, before either
 // qualification/source SQL or object publication can begin.
 err:=verifySemanticSceneStageAnchor(ctx,auth.Identity{},semanticSceneStageAnchor{}.project,Request{},semanticSceneStageAnchor{})
 if !errors.Is(err,context.Canceled){t.Fatalf("cancel: %v",err)}
}
