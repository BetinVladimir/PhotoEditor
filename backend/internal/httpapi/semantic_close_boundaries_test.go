package httpapi

import (
    "testing"
    "github.com/google/uuid"
    "photoeditor/backend/internal/canonical"
    "photoeditor/backend/internal/domain"
    "photoeditor/backend/internal/idempotency"
    "photoeditor/backend/internal/semanticreview"
)

func TestCloseHTTPResponseRefusesUnsafeOrUnincrementedRevision(t *testing.T) {
    batch:=uuid.New()
    for _,revision:=range []string{"1","9007199254740992","9223372036854775807"} {
        raw,err:=canonical.Marshal(map[string]any{"run_id":uuid.New(),"workflow_id":uuid.New(),"revision":revision,"state":"completed","outputs":[]any{},"error_code":nil})
        if err!=nil { t.Fatal(err) }
        _,err=semanticCloseResponse(idempotency.Response{Status:200,Body:raw,ResourceID:&batch})
        if err==nil || domain.AsError(err).HTTPStatus!=503 { t.Fatal(revision,err) }
    }
}

func TestCloseAdmissionErrorsAreRecoverableClientConflicts(t *testing.T) {
    for _,test:=range []struct{err error;code string;status int}{
        {semanticreview.ErrReviewIncomplete,"REVIEW_INCOMPLETE",409},
        {semanticreview.ErrConflict,"REVISION_CONFLICT",409},
        {semanticreview.ErrInvalid,"VALIDATION_FAILED",422},
        {semanticreview.ErrUnavailable,"DEPENDENCY_UNAVAILABLE",503},
    } {
        result:=domain.AsError(semanticReviewError(test.err))
        if result.Code!=test.code || result.HTTPStatus!=test.status { t.Fatal(result) }
    }
}
