package interchange

import (
 "context"
 "crypto/sha256"
 "encoding/hex"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
)

type ImportRequest struct {
 ProjectID uuid.UUID `json:"project_id"`
 Direction string `json:"direction"`
 Format string `json:"format"`
 Bundle v2input.Ref `json:"bundle"`
}
type ParseParams struct {
 SchemaVersion int `json:"schema_version"`
 Request ImportRequest `json:"request"`
 MaxExpandedBytes int64 `json:"max_expanded_bytes"`
 MaxFiles int `json:"max_files"`
}
type ProofTransaction interface {QueryRow(context.Context,string,...any)pgx.Row}
type ParseProofScope struct {Tenant,Run,Task uuid.UUID;Params v2input.Ref}

func supportedParseFormat(format string) bool {
 return format=="photoeditor_bundle"||format=="xmp"||format=="captureone_sidecar"
}

func CompileParseParams(params ParseParams)([]byte,error){
 if params.SchemaVersion!=1||params.Request.ProjectID==uuid.Nil||params.Request.Direction!="import"||!supportedParseFormat(params.Request.Format)||params.Request.Bundle.ArtifactID==uuid.Nil||params.MaxExpandedBytes<1||params.MaxExpandedBytes>128<<30||params.MaxFiles<1||params.MaxFiles>30000 {return nil,domain.NewError("VALIDATION_FAILED",422)}
 if _,err:=domain.ParseDigest(params.Request.Bundle.SHA256);err!=nil{return nil,domain.NewError("VALIDATION_FAILED",422)}
 return canonical.Marshal(params)
}

// Caller owns the authorized tenant transaction and rolls it back on error.
// Saving proof does not create a run/task or activate/dispatch its graph.
func SaveParseProof(ctx context.Context,tx ProofTransaction,who auth.Identity,scope ParseProofScope,params ParseParams)error{
 if err:=ctx.Err();err!=nil{return err}
 if tx==nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil||scope.Tenant==uuid.Nil||scope.Run==uuid.Nil||scope.Task==uuid.Nil||scope.Params.ArtifactID==uuid.Nil{return domain.NewError("VALIDATION_FAILED",422)}
 raw,err:=CompileParseParams(params);if err!=nil{return err}
 sum:=sha256.Sum256(raw)
 if hex.EncodeToString(sum[:])!=scope.Params.SHA256{return domain.NewError("VALIDATION_FAILED",422)}
 var accepted bool
 err=tx.QueryRow(ctx,`SELECT photo.save_v30_interchange_parse_proof($1,$2,$3,$4,$5,$6,$7,$8,$9)`,scope.Tenant,who.UserID,who.SessionID,params.Request.ProjectID,scope.Run,scope.Task,scope.Params.ArtifactID,scope.Params.SHA256,string(raw)).Scan(&accepted)
 if err!=nil{return err}
 if !accepted{return domain.NewError("INTERCHANGE_SOURCE_STALE",409)}
 return nil
}
