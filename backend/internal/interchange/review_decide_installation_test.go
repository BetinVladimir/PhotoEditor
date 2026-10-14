package interchange

import (
 "testing"

 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/v2draft"
)

type importReviewDraftObjects struct{v2draft.DraftStore}
type importReviewPublishObjects struct{artifactjson.Objects}

func TestImportApprovalRequiresSameDatabaseAndBucketAndBothObjectPorts(t *testing.T){
 db:=&pgxpool.Pool{};source:=Service{DB:db}
 draftObjects:=&importReviewDraftObjects{};publishObjects:=&importReviewPublishObjects{}
 drafts:=v2draft.Service{DB:db,Bucket:"photos",Objects:draftObjects}
 if !source.importApprovalInstalled("photos",publishObjects,drafts){t.Fatal("complete approval installation rejected")}
 if source.importApprovalInstalled("other",publishObjects,drafts){t.Fatal("split bucket accepted")}
 if source.importApprovalInstalled("photos",nil,drafts){t.Fatal("missing publisher accepted")}
 drafts.Objects=nil
 if source.importApprovalInstalled("photos",publishObjects,drafts){t.Fatal("missing parsed reader accepted")}
 drafts.Objects=draftObjects;drafts.DB=&pgxpool.Pool{}
 if source.importApprovalInstalled("photos",publishObjects,drafts){t.Fatal("split database accepted")}
}

func TestReviewDocumentPublisherRequiredForRejectAndClosePaths(t *testing.T){
 publisher:=&importReviewPublishObjects{}
 if !reviewDocumentInstalled("photos",publisher){t.Fatal("installed review publisher refused")}
 if reviewDocumentInstalled("",publisher)||reviewDocumentInstalled("photos",nil){t.Fatal("review document could publish without bucket or object port")}
}
