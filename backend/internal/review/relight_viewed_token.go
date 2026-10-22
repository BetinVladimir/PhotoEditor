package review

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
)

var ErrRelightViewedGrant = errors.New("relight viewed grant invalid")

// RelightViewedGrant attests that the authenticated client acknowledged displaying
// this exact preview pair. It cannot prove human attention. Issuance and decision
// admission must independently re-read live authority and physical catalogue pins.
type RelightViewedGrant struct {
 Version int `json:"version"`
 Kind string `json:"kind"`
 Revision int64 `json:"revision"`
 Before PendingPreviewGrant `json:"before"`
 After PendingPreviewGrant `json:"after"`
 ExpiresUnix int64 `json:"expires_unix"`
}

func (g RelightViewedGrant) valid(now time.Time) bool {
 b,a:=g.Before,g.After
 return g.Version==1 && g.Kind=="image.relight" && g.Revision==1 &&
  b.valid(now) && a.valid(now) && b.Side=="before" && a.Side=="after" &&
  b.Role=="image" && a.Role=="image" && b.Tenant==a.Tenant && b.Review==a.Review &&
  b.User==a.User && b.Session==a.Session && b.Variant==a.Variant &&
  b.Source!=a.Source && b.Artifact!=a.Artifact &&
  g.ExpiresUnix>now.Unix() && g.ExpiresUnix<=b.ExpiresUnix && g.ExpiresUnix<=a.ExpiresUnix
}

// MatchesRelightViewedGrant checks the locked review's immutable source pair.
// A valid token is insufficient without current session/ACL/qualification checks.
func MatchesRelightViewedGrant(g RelightViewedGrant, tenant, reviewID, user, session uuid.UUID,
 revision int64, base, candidate ArtifactRef, now time.Time) bool {
 return g.valid(now) && g.Before.Tenant==tenant && g.Before.Review==reviewID &&
  g.Before.User==user && g.Before.Session==session && g.Revision==revision &&
  base.valid() && candidate.valid() && g.Before.Source==base.ID && g.Before.SourceSHA==base.SHA256 &&
  g.After.Source==candidate.ID && g.After.SourceSHA==candidate.SHA256
}

func SealRelightViewedGrant(key []byte, grant RelightViewedGrant, now time.Time) (string, error) {
	if len(key) < 32 || !grant.valid(now) {
		return "", ErrRelightViewedGrant
	}
	data, err := json.Marshal(grant)
	if err != nil || len(data) > 3072 {
		return "", ErrRelightViewedGrant
	}
	payload := base64.RawURLEncoding.EncodeToString(data)
	signature := auth.MAC(key, "relight-viewed-pair-v1", payload)
	return payload + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func OpenRelightViewedGrant(key []byte, token string, now time.Time) (RelightViewedGrant, error) {
	var zero RelightViewedGrant
	if len(key) < 32 || len(token) > 4200 || strings.Count(token, ".") != 1 {
		return zero, ErrRelightViewedGrant
	}
	parts := strings.SplitN(token, ".", 2)
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(signature) != 32 || !auth.Equal(signature, auth.MAC(key, "relight-viewed-pair-v1", parts[0])) {
		return zero, ErrRelightViewedGrant
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || len(data) > 3072 || base64.RawURLEncoding.EncodeToString(data) != parts[0] {
		return zero, ErrRelightViewedGrant
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var grant RelightViewedGrant
	if decoder.Decode(&grant) != nil || decoder.Decode(new(any)) != io.EOF || !grant.valid(now) {
		return zero, ErrRelightViewedGrant
	}
	return grant, nil
}

// IssueRelightViewedAcknowledgement accepts only server-issued pending-image
// grants. The caller supplies refs from the live locked relight review, never
// client assertions, and must verify the current preview catalogue separately.
func IssueRelightViewedAcknowledgement(key []byte, beforeToken, afterToken string,
 tenant, reviewID, user, session uuid.UUID, revision int64, base, candidate ArtifactRef,
 now time.Time) (string, error) {
 before,err:=OpenPendingPreviewGrant(key,beforeToken,now)
 if err!=nil{return "",ErrRelightViewedGrant}
 after,err:=OpenPendingPreviewGrant(key,afterToken,now)
 if err!=nil{return "",ErrRelightViewedGrant}
 expires:=before.ExpiresUnix;if after.ExpiresUnix<expires{expires=after.ExpiresUnix}
 grant:=RelightViewedGrant{Version:1,Kind:"image.relight",Revision:revision,Before:before,After:after,ExpiresUnix:expires}
 if !MatchesRelightViewedGrant(grant,tenant,reviewID,user,session,revision,base,candidate,now){return "",ErrRelightViewedGrant}
 return SealRelightViewedGrant(key,grant,now)
}
