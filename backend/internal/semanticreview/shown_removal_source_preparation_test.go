package semanticreview

import (
 "bytes"
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
)

func TestShownRemovalSourceReadOwnsVerifiedBytes(t *testing.T) {
 sources, body := documentSources()
 source := sources.Objects[0]
 got, err := readShownRemovalSourceDocument(context.Background(), source, source.Ref, source.Contract, func(context.Context, PlanSource) ([]byte,error) { return body,nil })
 if err != nil || !bytes.Equal(got,body) { t.Fatal("valid source refused",err) }
 before := append([]byte(nil),got...)
 body[0] ^= 1
 if !bytes.Equal(got,before) { t.Fatal("source reader retained caller bytes") }
}

func TestShownRemovalSourceReadCancellationAndRefusals(t *testing.T) {
 for _, mode := range []string{"cancel_before","cancel_during","cancel_with_error","wrong_hash","content_hash","wrong_size","reader_error","nil_reader","bucket","key","version","null_version","media","contract","ref","zero_ref","small","large"} {
  t.Run(mode,func(t *testing.T) {
   sources, body := documentSources()
   source := sources.Objects[0]
   expected,contract:=source.Ref,source.Contract
   ctx,cancel := context.WithCancel(context.Background());defer cancel()
   called:=false
   failure:=errors.New("storage failed")
   read:=PlanObjectReader(func(context.Context,PlanSource)([]byte,error){
    called=true
    if mode=="cancel_during" || mode=="cancel_with_error" {cancel()}
    if mode=="reader_error" || mode=="cancel_with_error" {return nil,failure}
    if mode=="content_hash" { bad:=append([]byte(nil),body...);bad[0]^=1;return bad,nil }
    return body,nil
   })
   want:=ErrInvalid
   switch mode {
   case "cancel_before":cancel();want=context.Canceled
   case "cancel_during","cancel_with_error":want=context.Canceled
   case "wrong_hash":source.Ref.SHA256="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
   case "wrong_size":source.Bytes++
   case "reader_error":want=failure
   case "nil_reader":read=nil
   case "bucket":source.Bucket=""
   case "key":source.Key=""
   case "version":source.Version=""
   case "null_version":source.Version="null"
   case "media":source.MediaType="image/png"
   case "contract":source.Contract="MaskRef"
   case "ref":source.Ref.ArtifactID=uuid.New()
   case "zero_ref":source.Ref.ArtifactID=uuid.Nil;expected.ArtifactID=uuid.Nil
   case "small":source.Bytes=1
   case "large":source.Bytes=1048577
   }
   got,err:=readShownRemovalSourceDocument(ctx,source,expected,contract,read)
   if !errors.Is(err,want) || got!=nil {t.Fatal("source refusal mismatch",got,err)}
   shouldRead:=mode=="cancel_during" || mode=="cancel_with_error" || mode=="wrong_size" || mode=="content_hash" || mode=="reader_error"
   if called!=shouldRead {t.Fatal("unexpected object read",mode,called)}
  })
 }
}
