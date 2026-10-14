package v2learning
import (
 "context"
 "errors"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)
type FrozenEvaluateBackgroundVerifier interface {
 VerifyBackground(context.Context,LearningEvaluateLease,v2draft.FrozenBackgroundAsset)error
}
// Shares the already installed style reader's scheduler DB and bucket scope.
func(s SQLLearningEvaluateStyleSource)VerifyBackground(ctx context.Context,lease LearningEvaluateLease,pin v2draft.FrozenBackgroundAsset)error{
 if err:=ctx.Err();err!=nil{return err}
 if s.DB==nil||s.Bucket==""||!validLearningEvaluateLease(lease)||!validTrainingBackgroundPin(&pin,"",v2input.Ref{}){return ErrInvalidTrainingDraft}
 var found v2input.Ref;var object objectstore.Object;var bucket string
 err:=s.DB.QueryRow(ctx,`SELECT artifact_id,sha256,bytes,bucket,object_key,object_version,media_type FROM photo.v23_evaluate_background_source($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,lease.TenantID,lease.RunID,lease.ExpansionID,lease.Token,pin.StyleVersionID,pin.Style.ArtifactID,pin.Style.SHA256,pin.Source.ArtifactID,pin.Source.SHA256,pin.PortableAssetID,pin.ExpectedBytes,pin.MediaType).Scan(&found.ArtifactID,&found.SHA256,&object.Bytes,&bucket,&object.Key,&object.VersionID,&object.ContentType)
 if errors.Is(err,pgx.ErrNoRows){return ErrTrainingDraftConflict};if err!=nil{return err}
 if err:=ctx.Err();err!=nil{return err}
 return validateEvaluateBackgroundObject(pin,found,bucket,s.Bucket,object)
}

// SQL authority and the encoded object descriptor must agree before task inputs
// are admitted. Object bytes are verified independently by the worker reader.
func validateEvaluateBackgroundObject(pin v2draft.FrozenBackgroundAsset,found v2input.Ref,bucket,expectedBucket string,object objectstore.Object)error{
 if !validTrainingBackgroundPin(&pin,"",v2input.Ref{})||expectedBucket==""||found!=pin.Source||bucket!=expectedBucket||object.Key==""||object.VersionID==""||object.VersionID=="null"||object.Bytes!=pin.ExpectedBytes||object.ContentType!=pin.MediaType{return ErrInvalidTrainingDraft}
 return nil
}
