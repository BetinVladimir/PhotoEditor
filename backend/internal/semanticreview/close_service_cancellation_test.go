package semanticreview

import (
    "context"
    "errors"
    "testing"

    "github.com/google/uuid"
    "photoeditor/backend/internal/auth"
)

func TestCancelledCloseServiceDoesNotResolveReplayOrPrepare(t *testing.T) {
    ctx,cancel:=context.WithCancel(context.Background());cancel()
    called:=false
    service:=&CloseService{Prepare:func(context.Context,auth.Identity,uuid.UUID,uuid.UUID,ReviewClose)(PreparedClose,error){called=true;return PreparedClose{},nil}}
    response,err:=service.Close(ctx,auth.Identity{},uuid.Nil,uuid.Nil,"",ReviewClose{})
    if !errors.Is(err,context.Canceled) || called || len(response.Body)!=0 || response.ResourceID!=nil { t.Fatal(response,err,called) }
}
