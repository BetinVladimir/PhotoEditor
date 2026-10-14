package v2optical

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/v2input"
)

type multiIntentIdentity struct {
	Run         uuid.UUID
	Route, Hash string
}

func multiRunID(tenant, project, actor uuid.UUID, operation, key string) uuid.UUID {
	return uuid.NewSHA1(tenant, []byte("V29.Multi:"+operation+":"+project.String()+":"+actor.String()+":"+key))
}

// Matches claim/admission/recovery SQL. The key fixes resource identity; the
// hash independently binds exact intent including ordered frames and session.
func compileMultiIntentIdentity(who auth.Identity, tenant, project uuid.UUID, operation, key string, intent MultiIntent) (multiIntentIdentity, error) {
	var zero multiIntentIdentity
	if tenant == uuid.Nil || project == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || !idempotency.ValidKey(key) {
		return zero, v2input.ErrInvalid
	}
	raw, err := canonical.Marshal(intent)
	if err != nil {
		return zero, err
	}
	intent, err = DecodeMultiIntent(operation, raw)
	if err != nil {
		return zero, err
	}
	hash, err := canonical.Hash(struct {
		Project   uuid.UUID   `json:"project_id"`
		Session   uuid.UUID   `json:"auth_session_id"`
		Operation string      `json:"operation"`
		Intent    MultiIntent `json:"intent"`
	}{project, who.SessionID, operation, intent})
	if err != nil {
		return zero, err
	}
	return multiIntentIdentity{Run: multiRunID(tenant, project, who.UserID, operation, key), Route: "V29.Multi:" + operation + ":" + project.String(), Hash: hash}, nil
}
