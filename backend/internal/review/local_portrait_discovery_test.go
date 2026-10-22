package review
import("context";"errors";"testing";"github.com/google/uuid";"photoeditor/backend/internal/auth")
func TestLocalPortraitDiscoveryMissingInstallation(t *testing.T){
 var service *Service
 if _,err:=service.DiscoverLocalPortraitResult(context.Background(),auth.Identity{},uuid.New(),uuid.New());err==nil{t.Fatal("missing installation admitted discovery")}
 if _,err:=(&Service{}).DiscoverLocalPortraitResult(context.Background(),auth.Identity{},uuid.New(),uuid.Nil);err==nil{t.Fatal("empty run admitted discovery")}
}
func TestLocalPortraitDiscoveryCancellationDoesNotExposeResult(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=(&Service{}).DiscoverLocalPortraitResult(ctx,auth.Identity{},uuid.New(),uuid.New());!errors.Is(err,context.Canceled){t.Fatal("cancelled discovery was not withdrawn",err)}
}
