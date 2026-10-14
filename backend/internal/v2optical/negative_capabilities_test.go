package v2optical

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2qualification"
)
func TestNegativeCapabilitiesUnconfiguredProjectAndCancellation(t *testing.T) {
 p:=&NegativeProjects{services:map[[2]uuid.UUID]*NegativeService{}}
 if _,err:=p.ProjectCapabilities(context.Background(),auth.Identity{},uuid.New(),uuid.New());err!=v2qualification.ErrUnqualified { t.Fatal("unconfigured project",err) }
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=p.ProjectCapabilities(ctx,auth.Identity{},uuid.New(),uuid.New());err!=context.Canceled { t.Fatal("cancelled request",err) }
 var absent *NegativeProjects
 if _,err:=absent.ProjectCapabilities(context.Background(),auth.Identity{},uuid.New(),uuid.New());err!=v2qualification.ErrUnqualified { t.Fatal("absent deployment",err) }
}
