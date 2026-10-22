package httpapi

import (
 "context"
 "net/http"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2portrait"
)

func relightRequestContext(r *http.Request) (context.Context, error) {
 values := r.Header.Values("Relight-Replay-Only")
 if len(values)==0 { return r.Context(), nil }
 if len(values)!=1 || values[0]!="true" {
  return nil, domain.NewError("VALIDATION_FAILED", 422)
 }
 return v2portrait.WithRelightReplayOnly(r.Context()), nil
}
