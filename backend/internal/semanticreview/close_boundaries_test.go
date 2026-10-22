package semanticreview

import (
    "bytes"
    "errors"
    "strings"
    "testing"

    "github.com/google/uuid"
    "photoeditor/backend/internal/canonical"
)

func TestCloseRevisionMustRemainSafeAfterIncrement(t *testing.T) {
    for _, revision := range []string{"1", "9007199254740990"} {
        request := ReviewClose{ExpectedRevision: revision, PlanSHA: strings.Repeat("a",64)}
        raw, err := canonical.Marshal(request)
        if err != nil { t.Fatal(err) }
        if _, err = DecodeReviewClose(raw); err != nil { t.Fatal(revision, err) }
        if _, err = BuildCloseProof(uuid.New(),request,request.PlanSHA,revision,"waiting_review",[]LockedTarget{{ID:uuid.New(),State:"queued"}}); err != nil { t.Fatal(revision, err) }
    }
    for _, revision := range []string{"0","01","-1","1.0","9007199254740991","9007199254740992","9223372036854775807"} {
        raw, err := canonical.Marshal(ReviewClose{ExpectedRevision:revision,PlanSHA:strings.Repeat("a",64)})
        if err != nil { t.Fatal(err) }
        if _, err = DecodeReviewClose(raw); !errors.Is(err,ErrInvalid) { t.Fatal(revision,err) }
    }
}

func TestCloseProofOwnsCanonicalFrozenParentAndPlan(t *testing.T) {
    run := uuid.New()
    request := ReviewClose{ExpectedRevision:"7",PlanSHA:strings.Repeat("a",64)}
    targets := []LockedTarget{{ID:uuid.New(),State:"queued"},{ID:uuid.New(),State:"rejected"}}
    first, err := BuildCloseProof(run,request,request.PlanSHA,"7","waiting_review",targets)
    if err != nil { t.Fatal(err) }
    second, err := BuildCloseProof(run,request,request.PlanSHA,"7","waiting_review",[]LockedTarget{targets[1],targets[0]})
    if err != nil || !bytes.Equal(first.Body,second.Body) || first.SHA256!=second.SHA256 || first.RequestSHA256!=second.RequestSHA256 { t.Fatal("order changed proof",err) }
    targets[0].State="proposed"
    if _,err=BuildCloseProof(run,request,request.PlanSHA,"7","waiting_review",targets); !errors.Is(err,ErrReviewIncomplete) { t.Fatal(err) }
    first.Body[0]='x'
    third,err:=BuildCloseProof(run,request,request.PlanSHA,"7","waiting_review",[]LockedTarget{targets[1]})
    if err!=nil || !bytes.Equal(second.Body,third.Body) { t.Fatal("caller changed proof",err) }
    for _, change := range []string{"parent","plan","revision"} {
        parent,modified:=run,request
        switch change { case "parent":parent=uuid.New();case "plan":modified.PlanSHA=strings.Repeat("b",64);case "revision":modified.ExpectedRevision="8" }
        hash,err:=HashCloseRequest(parent,modified)
        if err!=nil || hash==second.RequestSHA256 { t.Fatal(change,err) }
    }
}

func TestCloseProofRefusesIncompleteOrAmbiguousInventory(t *testing.T) {
    id:=uuid.New()
    request:=ReviewClose{ExpectedRevision:"1",PlanSHA:strings.Repeat("a",64)}
    cases:=map[string][]LockedTarget{
        "empty":nil,
        "nil_target":{{State:"rejected"}},
        "duplicate":{{ID:id,State:"queued"},{ID:id,State:"rejected"}},
        "unknown_state":{{ID:id,State:"invented"}},
    }
    for name,targets:=range cases { t.Run(name,func(t *testing.T){
        proof,err:=BuildCloseProof(uuid.New(),request,request.PlanSHA,"1","waiting_review",targets)
        if !errors.Is(err,ErrInvalid) || len(proof.Body)!=0 || proof.SHA256!="" || proof.RequestSHA256!="" { t.Fatal(proof,err) }
    }) }
}
