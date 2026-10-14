package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestOriginalPersonInputAuthorityScopeSealAndCancellation(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	r := Request{TenantID: uuid.New(), ActorID: who.UserID}
	s := SQLTransferSourceReader{ProjectID: uuid.New(), Bucket: "owned"}
	p := nativePersonInputAuthority{tenant: r.TenantID, project: s.ProjectID, actor: who.UserID, session: who.SessionID, bucket: s.Bucket, binding: r.Intent.Reference, policy: qualificationPolicySnapshot{tenant: r.TenantID, project: s.ProjectID, actor: who.UserID, session: who.SessionID, body: `[{}]`}}
	var err error
	p.seal, err = nativePersonInputAuthorityDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.validateNativePersonInputAuthority(context.Background(), who, r, p); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"tenant", "project", "actor", "session", "bucket", "binding", "root", "pixels", "report", "body", "expected", "evidence", "qualification", "object", "policy"} {
		changed := p
		switch kind {
		case "tenant":
			changed.tenant = uuid.New()
		case "project":
			changed.project = uuid.New()
		case "actor":
			changed.actor = uuid.New()
		case "session":
			changed.session = uuid.New()
		case "bucket":
			changed.bucket = "foreign"
		case "binding":
			changed.binding.AssetVersionID = uuid.New()
		case "root":
			changed.root.ArtifactID = uuid.New()
		case "pixels":
			changed.pixels.ArtifactID = uuid.New()
		case "report":
			changed.report.ArtifactID = uuid.New()
		case "body":
			changed.body = "altered"
		case "expected":
			changed.expected = "altered"
		case "evidence":
			changed.evidence = "altered"
		case "qualification":
			changed.qualification = "altered"
		case "object":
			changed.objects[1].VersionID = "other"
		case "policy":
			changed.policy.body = `[{"altered":true}]`
		}
		tx := &manualLockTx{role: "photo_scheduler"}
		if err = s.lockNativePersonInputAuthority(context.Background(), tx, who, r, changed); err == nil || len(tx.calls) != 0 {
			t.Fatalf("%s changed authority reached SQL: %v", kind, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = s.readNativePersonInputAuthority(ctx, who, r, v2input.Ref{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err = s.lockNativePersonInputAuthority(ctx, nil, who, r, p); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for _, role := range []string{"photo_api", "photo_worker", "photo_publisher", "photo_gateway"} {
		tx := &manualLockTx{role: role}
		if err = s.lockNativePersonInputAuthority(context.Background(), tx, who, r, p); !errors.Is(err, v2input.ErrInvalid) || len(tx.calls) != 1 {
			t.Fatal("wrong role advanced", role, err)
		}
	}
	for fail := 2; fail <= 7; fail++ {
		tx := &manualLockTx{role: "photo_scheduler", failAt: fail}
		if err = s.lockNativePersonInputAuthority(context.Background(), tx, who, r, p); !errors.Is(err, v2input.ErrConflict) {
			t.Fatal("refused SQL authority advanced", fail, err)
		}
	}
}
