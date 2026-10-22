package semanticreview

import (
 "context"
 "testing"
 "github.com/google/uuid"
)

func TestShownCommitRejectsInvalidIdempotencyKeyBeforeDatabase(t *testing.T) {
 tx:=&shownLockTX{}
 response,err:=CommitShownExposure(context.Background(),tx,uuid.New(),uuid.New(),uuid.New(),uuid.New(),uuid.New(),
 "",PreparedReview{},nil,ApplyDecision{},PublishedDecision{},nil)
 if err==nil || tx.calls!=0 || response.Body!=nil || response.ResourceID!=nil {t.Fatal("invalid mutation reached database")}
}
