package semanticreview

import (
    "bytes"
    "context"
    "errors"
    "strings"
    "testing"

    "github.com/google/uuid"
    "photoeditor/backend/internal/auth"
    "photoeditor/backend/internal/v2input"
)

func closePublicationProof(t *testing.T) CloseProof {
    t.Helper()
    request:=ReviewClose{ExpectedRevision:"3",PlanSHA:strings.Repeat("a",64)}
    proof,err:=BuildCloseProof(uuid.New(),request,request.PlanSHA,"3","waiting_review",[]LockedTarget{{ID:uuid.New(),State:"queued"}})
    if err!=nil { t.Fatal(err) }
    return proof
}

func TestClosePublicationOwnsProofAcrossCallback(t *testing.T) {
    proof:=closePublicationProof(t)
    original:=append([]byte(nil),proof.Body...)
    owner,artifact:=uuid.New(),uuid.New()
    var received []byte
    result,err:=publishCloseProof(context.Background(),owner,proof,func(_ context.Context,body []byte)(v2input.Ref,error){
        if !bytes.Equal(body,original) { t.Fatal("changed publication bytes") }
        received=body
        proof.Body[0]='x'
        return v2input.Ref{ArtifactID:artifact,SHA256:proof.SHA256},nil
    })
    if err!=nil || result.BatchID!=owner || result.Ref.ArtifactID!=artifact || !bytes.Equal(result.Proof.Body,original) { t.Fatal(result,err) }
    received[0]='y'
    if !bytes.Equal(result.Proof.Body,original) { t.Fatal("publisher retained result proof bytes") }
}

func TestClosePublicationRefusesFailureCancellationAndInvalidReceipt(t *testing.T) {
    for _,mode:=range []string{"cancel_before","cancel_during","failure","nil_artifact","wrong_sha","nil_owner","nil_publisher","bad_proof_sha","bad_request_sha"} {
        t.Run(mode,func(t *testing.T){
            proof:=closePublicationProof(t)
            owner:=uuid.New()
            ctx,cancel:=context.WithCancel(context.Background());defer cancel()
            calls:=0
            sentinel:=errors.New("publication failed")
            var publish func(context.Context,[]byte)(v2input.Ref,error)=func(context.Context,[]byte)(v2input.Ref,error){
                calls++
                if mode=="cancel_during" { cancel() }
                if mode=="failure" { return v2input.Ref{},sentinel }
                ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:proof.SHA256}
                if mode=="nil_artifact" { ref.ArtifactID=uuid.Nil }
                if mode=="wrong_sha" { ref.SHA256=strings.Repeat("0",64) }
                return ref,nil
            }
            switch mode { case "cancel_before":cancel();case "nil_owner":owner=uuid.Nil;case "nil_publisher":publish=nil;case "bad_proof_sha":proof.SHA256=strings.Repeat("0",64);case "bad_request_sha":proof.RequestSHA256="invalid" }
            result,err:=publishCloseProof(ctx,owner,proof,publish)
            if err==nil || result.BatchID!=uuid.Nil || result.Ref.ArtifactID!=uuid.Nil || len(result.Proof.Body)!=0 { t.Fatal("partial success",result,err) }
            expectedCalls:=1
            switch mode { case "cancel_before","nil_owner","nil_publisher","bad_proof_sha","bad_request_sha":expectedCalls=0 }
            if calls!=expectedCalls { t.Fatal("unexpected publication",calls) }
            if (mode=="cancel_before" || mode=="cancel_during") && !errors.Is(err,context.Canceled) { t.Fatal(err) }
            if mode=="failure" && !errors.Is(err,sentinel) { t.Fatal(err) }
        })
    }
}

func TestClosePreparationAndPublicationStopOnCancelledContext(t *testing.T) {
    ctx,cancel:=context.WithCancel(context.Background());cancel()
    if _,_,err:=PrepareScopedCloseReview(ctx,nil,auth.Identity{},uuid.Nil,uuid.Nil,ReviewClose{},nil,0,"","");!errors.Is(err,context.Canceled) { t.Fatal(err) }
    if _,err:=PublishClose(ctx,nil,nil,"",uuid.Nil,uuid.Nil,uuid.Nil,uuid.Nil,PreparedReview{},nil,ReviewClose{});!errors.Is(err,context.Canceled) { t.Fatal(err) }
}
