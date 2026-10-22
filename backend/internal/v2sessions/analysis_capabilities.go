package v2sessions

import (
 "context"
 "encoding/json"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
)

type SessionAnalysisCapabilities struct {
 SchemaVersion int `json:"schema_version"`
 TenantID uuid.UUID `json:"tenant_id"`
 ProjectID uuid.UUID `json:"project_id"`
 SessionID uuid.UUID `json:"session_id"`
 RequestSHA string `json:"request_sha"`
 ModelLockSHA string `json:"model_lock_sha"`
 ExpiresAt time.Time `json:"expires_at"`
}

// AnalysisCapabilities is a read-only advisory for a NEW analysis attempt. It
// executes the same qualification, calibration and relational preflight as fresh
// admission, without probing/creating a receipt or reserving/publishing a graph.
// The actual Analyze path still probes immutable recovery before fresh preflight.
func(s Service) AnalysisCapabilities(ctx context.Context, who auth.Identity, tid, sessionID uuid.UUID, req AnalysisRequest)(SessionAnalysisCapabilities,error){
 var out SessionAnalysisCapabilities
 ctx,cancel:=context.WithTimeout(ctx,8*time.Second);defer cancel()
 hash,err:=canonical.Hash(req);if err!=nil{return out,err}
 preflight,err:=s.Preflight(ctx,who,tid,sessionID,req);if err!=nil{return out,err}
 var scope struct{ProjectID uuid.UUID `json:"project_id"`;SessionID uuid.UUID `json:"session_id"`}
 if json.Unmarshal(preflight.Context,&scope)!=nil||scope.ProjectID==uuid.Nil||scope.SessionID!=sessionID{return out,domain.NewError("INTERNAL_ERROR",500)}
 if err=ctx.Err();err!=nil{return out,err}
 return SessionAnalysisCapabilities{1,tid,scope.ProjectID,sessionID,hash,preflight.ModelLockSHA,time.Now().UTC().Add(15*time.Second)},nil
}
