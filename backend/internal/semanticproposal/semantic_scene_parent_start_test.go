package semanticproposal

import (
 "context"
 "errors"
 "testing"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)

func TestOrdinarySceneStartCancellationCannotPublishPrivateChild(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=(TransferDraftPreparer{}).prepareOrdinarySceneParent(ctx,auth.Identity{},Request{},"pending");!errors.Is(err,context.Canceled){t.Fatalf("canceled start: %v",err)}
}

func TestOrdinarySceneStartRefusesIncompleteInstallationBeforePublication(t *testing.T){
 if err:=(TransferDraftPreparer{}).prepareOrdinarySceneParent(context.Background(),auth.Identity{},Request{},"pending");!errors.Is(err,v2input.ErrInvalid){t.Fatalf("missing ordinary installation accepted: %v",err)}
}
