package media

import (
 "context"
 "errors"
 "time"
 "github.com/jackc/pgx/v5"
 "github.com/google/uuid"
)

type rendererPolicyDB interface { QueryRow(context.Context,string,...any) pgx.Row }

const CurrentRendererPolicy="declared_encoding_alpha8_icc_v2"
const LegacyRendererPolicy="legacy_v1"

func ResolveRendererPolicy(values ...string)(string,error) {
 if len(values)==0 || len(values)==1 && values[0]=="" {return CurrentRendererPolicy,nil}
 if len(values)!=1 || values[0]!=CurrentRendererPolicy && values[0]!=LegacyRendererPolicy {return "",errors.New("unknown media renderer policy")}
 return values[0],nil
}

func ProfileHashForPolicy(variant,srgbSHA,policy string)(string,error) {
 if policy=="" {return "",errors.New("empty frozen media renderer policy")}
 resolved,err:=ResolveRendererPolicy(policy);if err!=nil{return "",err}
 if resolved==LegacyRendererPolicy{return LegacyProfileHash(variant,srgbSHA)}
 return ProfileHash(variant,srgbSHA)
}

func PolicyForRun(ctx context.Context,db rendererPolicyDB,tenant,run uuid.UUID)(string,error) {
 if err:=ctx.Err();err!=nil{return "",err}
 if db==nil || tenant==uuid.Nil || run==uuid.Nil {return "",errors.New("invalid media policy scope")}
 var policy *string
 bounded,stop:=context.WithTimeout(ctx,5*time.Second);defer stop()
 if err:=db.QueryRow(bounded,"SELECT photo.media_renderer_policy_for_run($1,$2)",tenant,run).Scan(&policy);err!=nil{return "",err}
 if err:=bounded.Err();err!=nil{return "",err}
 if policy==nil{return CurrentRendererPolicy,nil}
 if *policy=="" {return "",errors.New("empty frozen media renderer policy")}
 return ResolveRendererPolicy(*policy)
}

func RequireRendererPolicy(ctx context.Context,db rendererPolicyDB) error {
 if err:=ctx.Err();err!=nil{return err}
 if db==nil {return errors.New("media renderer policy unavailable")}
 var ready bool
 bounded,stop:=context.WithTimeout(ctx,5*time.Second);defer stop()
 if err:=db.QueryRow(bounded,"SELECT photo.media_renderer_policy_ready()").Scan(&ready);err!=nil{return err}
 if err:=bounded.Err();err!=nil{return err}
 if !ready{return errors.New("media renderer policy not installed")}
 return nil
}

// PolicyForRetry reads the durable command's policy before its run exists.
// A missing, expired or foreign lease never becomes a fresh-command default.
func PolicyForRetry(ctx context.Context,db rendererPolicyDB,tenant,event,token uuid.UUID)(string,error) {
 if err:=ctx.Err();err!=nil{return "",err}
 if db==nil || tenant==uuid.Nil || event==uuid.Nil || token==uuid.Nil {return "",errors.New("invalid media retry policy scope")}
 bounded,stop:=context.WithTimeout(ctx,5*time.Second);defer stop()
 var policy *string
 if err:=db.QueryRow(bounded,"SELECT photo.media_renderer_policy_for_retry($1,$2,$3)",tenant,event,token).Scan(&policy);err!=nil{return "",err}
 if err:=bounded.Err();err!=nil{return "",err}
 if policy==nil || *policy=="" {return "",errors.New("media retry policy unavailable")}
 return ResolveRendererPolicy(*policy)
}

// PolicyForPreview selects an existing intent policy or its durable producer's
// policy. The subsequent save revalidates the complete source-bound intent.
func PolicyForPreview(ctx context.Context,db rendererPolicyDB,tenant,child,producer uuid.UUID)(string,error) {
 if err:=ctx.Err();err!=nil{return "",err}
 if db==nil || tenant==uuid.Nil || child==uuid.Nil || producer==uuid.Nil{return "",errors.New("invalid media preview policy scope")}
 bounded,stop:=context.WithTimeout(ctx,5*time.Second);defer stop()
 var policy *string
 if err:=db.QueryRow(bounded,"SELECT photo.media_renderer_policy_for_preview($1,$2,$3)",tenant,child,producer).Scan(&policy);err!=nil{return "",err}
 if err:=bounded.Err();err!=nil{return "",err}
 if policy==nil || *policy==""{return "",errors.New("media preview policy unavailable")}
 return ResolveRendererPolicy(*policy)
}
