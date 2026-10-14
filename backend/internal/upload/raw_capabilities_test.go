package upload

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
)
func TestRAWCapabilitiesRejectInvalidScopeAndMissingDatabase(t *testing.T){
 service:=Service{}
 for _,projectID:=range []uuid.UUID{uuid.Nil,uuid.New()}{
  out,err:=service.RAWCapabilities(context.Background(),auth.Identity{},uuid.New(),projectID)
  if err==nil||out.Available||out.Development!=nil{t.Fatal("unavailable scope advertised settings")}
 }
}
