package delivery

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
)
func TestWatermarkSourceRequiresExplicitVersionAndDependencies(t *testing.T){
 who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()};s:=Service{}
 if _,err:=s.ResolveWatermarkSource(context.Background(),who,uuid.New(),uuid.New(),uuid.New(),uuid.Nil,"photos");err==nil{t.Fatal("latest version fallback")}
 if _,err:=s.ResolveWatermarkSource(context.Background(),who,uuid.New(),uuid.New(),uuid.New(),uuid.New(),"photos");err==nil{t.Fatal("missing dependency")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=s.ResolveWatermarkSource(ctx,who,uuid.New(),uuid.New(),uuid.New(),uuid.New(),"photos");err!=context.Canceled{t.Fatal(err)}
}
