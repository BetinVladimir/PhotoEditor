package review

import (
 "context"
 "bytes"
 "crypto/sha256"
 "encoding/hex"
 "photoeditor/backend/internal/canonical"
 "encoding/json"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/domain"
)

type NegativeReviewPublication struct{
 Tenant,Task uuid.UUID
 Root,Candidate,Base ArtifactRef
 Params ArtifactRef
 ParamsJCS []byte
}
type NegativeReviewPublicationDB interface{QueryRow(context.Context,string,...any)pgx.Row}

// Scheduler-only publication; the caller must read original Params through its
// version-pinned object reader. SQL repeats canonical adoption and Params SHA.
func PublishNegativeReview(ctx context.Context,db NegativeReviewPublicationDB,request NegativeReviewPublication)(uuid.UUID,error){
 if err:=ctx.Err();err!=nil{return uuid.Nil,err}
 if db==nil||request.Tenant==uuid.Nil||request.Task==uuid.Nil||!request.Root.valid()||!request.Candidate.valid()||!request.Base.valid()||!request.Params.valid()||len(request.ParamsJCS)<2||len(request.ParamsJCS)>65536{return uuid.Nil,domain.NewError("INPUT_INVALID",422)}
 raw:=append([]byte(nil),request.ParamsJCS...)
 if err:=validateNegativePublicationParams(raw,request.Params,request.Base);err!=nil{return uuid.Nil,err}
 var id *uuid.UUID
 err:=db.QueryRow(ctx,`SELECT photo.create_v29_negative_review($1,$2,$3,$4,$5,$6)`,request.Tenant,request.Task,request.Root.ID,request.Candidate.ID,request.Base.ID,string(raw)).Scan(&id)
 if err!=nil{return uuid.Nil,err};if err:=ctx.Err();err!=nil{return uuid.Nil,err}
 if id==nil||*id==uuid.Nil{return uuid.Nil,domain.NewError("STALE_CANDIDATE_REVIEW",409)}
 return *id,nil
}
func validateNegativePublicationParams(raw []byte,params,base ArtifactRef)error{
 if err:=verifyNegativePublicationDigest(raw,params);err!=nil{return err}
 var doc struct{SchemaVersion int `json:"schema_version"`;Base struct{Source ArtifactRef `json:"source"`} `json:"base"`}
 if json.Unmarshal(raw,&doc)!=nil||doc.SchemaVersion!=1||doc.Base.Source!=base{return domain.NewError("INPUT_INVALID",422)}
 return nil
}

func verifyNegativePublicationDigest(raw []byte,params ArtifactRef)error{
 if !params.valid()||len(raw)<2||len(raw)>65536{return domain.NewError("INPUT_INVALID",422)}
 sum:=sha256.Sum256(raw)
 normalized,err:=canonical.Bytes(raw)
 if err!=nil||!bytes.Equal(normalized,raw)||hex.EncodeToString(sum[:])!=params.SHA256{return domain.NewError("ARTIFACT_HASH_MISMATCH",409)}
 return nil
}
