package workflow

import (
 "context"
 "errors"
 "strings"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)

type retryClaimRow func(...any)error
func(r retryClaimRow)Scan(dest ...any)error{return r(dest...)}
type retryClaimDB struct{event,token,tenant uuid.UUID;failure error;released int;deadline time.Time;variants string;policy string;afterPolicy func();releaseErr error}
func(d *retryClaimDB)QueryRow(ctx context.Context,q string,args ...any)pgx.Row{
 return retryClaimRow(func(dest ...any)error{
  switch {
  case strings.Contains(q,"claim_media_retry_job"):
   *(dest[0].(*uuid.UUID))=d.event;*(dest[1].(*uuid.UUID))=d.token;*(dest[2].(*uuid.UUID))=d.tenant
   *(dest[5].(*uuid.UUID))=uuid.New();variants:=d.variants;if variants==""{variants=`["thumbnail"]`};*(dest[6].(*[]byte))=[]byte(variants)
   *(dest[7].(*uuid.UUID))=uuid.New();*(dest[8].(*string))="photos";*(dest[13].(*string))="application/json";*(dest[16].(*string))="working_image"
   return nil
  case strings.Contains(q,"media_renderer_policy_for_retry"):
   if len(args)!=3||args[0]!=d.tenant||args[1]!=d.event||args[2]!=d.token{return errors.New("wrong policy lease scope")}
   if d.afterPolicy!=nil{d.afterPolicy()}
   if d.failure!=nil{return d.failure}
   policy:=d.policy;if policy==""{policy="legacy_v1"};*(dest[0].(**string))=&policy;return nil
  case strings.Contains(q,"release_media_retry_job"):
   if len(args)!=2||args[0]!=d.event||args[1]!=d.token{return errors.New("wrong release identity")}
   d.released++;d.releaseErr=ctx.Err();d.deadline,_=ctx.Deadline();*(dest[0].(*bool))=true;return nil
  }
  return errors.New("unexpected query")
 })
}
func TestMediaRetryClaimPolicyFailureReleasesClaimBeforeReturning(t *testing.T){
 failure:=errors.New("policy unavailable")
 db:=&retryClaimDB{event:uuid.New(),token:uuid.New(),tenant:uuid.New(),failure:failure}
 objects:=&artifactObjects{}
 lease,err:=(MediaRetryStore{DB:db,Objects:objects,Bucket:"photos"}).Claim(context.Background())
 if lease!=nil||!errors.Is(err,failure)||db.released!=1||objects.opened!=0{t.Fatal("failed claim leaked lease or read object",lease,err,db)}
 if db.deadline.IsZero()||time.Until(db.deadline)>5*time.Second{t.Fatal("release unbounded")}
}

func TestMediaRetryClaimMetadataAndReadFailureReleaseLease(t *testing.T){
 for _,variants:=range []string{"invalid-json",`["thumbnail"]`}{t.Run(variants,func(t *testing.T){
  db:=&retryClaimDB{event:uuid.New(),token:uuid.New(),tenant:uuid.New(),variants:variants}
  lease,err:=(MediaRetryStore{DB:db,Objects:&artifactObjects{},Bucket:"photos"}).Claim(context.Background())
  if lease!=nil||err==nil||db.released!=1||db.releaseErr!=nil{t.Fatal("failed claimed metadata/read retained lease",lease,err,db)}
 })}
}
func TestMediaRetryClaimCancellationDuringPolicyUsesIndependentCleanup(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());defer cancel()
 db:=&retryClaimDB{event:uuid.New(),token:uuid.New(),tenant:uuid.New(),afterPolicy:cancel}
 lease,err:=(MediaRetryStore{DB:db,Objects:&artifactObjects{},Bucket:"photos"}).Claim(ctx)
 if lease!=nil||!errors.Is(err,context.Canceled)||db.released!=1||db.releaseErr!=nil||db.deadline.IsZero(){t.Fatal("cancelled lookup lost independent release",lease,err,db)}
}
