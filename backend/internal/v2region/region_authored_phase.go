package v2region

// AuthoredRegionPhase is a compound-request phase, not a task/run status and
// not a substituted JobTicket. The authorized descriptor reader exposes no
// full-canvas object URL or mutable internal child configuration.
type AuthoredRegionPhase string

const (
	AuthoredRegionAdmitted   AuthoredRegionPhase = "admitted"
	AuthoredRegionRendering  AuthoredRegionPhase = "rendering"
	AuthoredRegionCropping   AuthoredRegionPhase = "cropping"
	AuthoredRegionReady      AuthoredRegionPhase = "ready"
	AuthoredRegionCancelling AuthoredRegionPhase = "cancelling"
	AuthoredRegionCancelled  AuthoredRegionPhase = "cancelled"
	AuthoredRegionStale      AuthoredRegionPhase = "stale"
	AuthoredRegionFailed     AuthoredRegionPhase = "failed"
)

// These facts are read under the compound store's ordered locks. They are not
// public request fields and cannot replace task/result/attempt/fence proofs.
// RenderCanonical and CropCanonical mean the independent exact identity-bound
// canonical/adoption checks succeeded, not merely a child status completed.
type authoredRegionFacts struct {
	Current          bool
	CancelRequested  bool
	RenderExists     bool
	RenderTerminal   bool
	RenderCanonical  bool
	CropExists       bool
	CropTerminal     bool
	CropCanonical    bool
	AllWritersClosed bool
	AllQuotaSettled  bool
}

// Cancellation/stale completion waits for actual writers and resource
// settlement. Never infer safe refund from a parent timeout or missing child.
func nextAuthoredRegionPhase(f authoredRegionFacts) (AuthoredRegionPhase, error) {
	if f.RenderCanonical && (!f.RenderExists || !f.RenderTerminal) || f.CropCanonical && (!f.CropExists || !f.CropTerminal || !f.RenderCanonical) || f.CropExists && (!f.RenderExists || f.Current && !f.CancelRequested && !f.RenderCanonical) || f.RenderTerminal && !f.RenderExists || f.CropTerminal && !f.CropExists {
		return "", ErrRegionInvalid
	}
	stopped := (!f.RenderExists || f.RenderTerminal) && (!f.CropExists || f.CropTerminal) && f.AllWritersClosed && f.AllQuotaSettled
	if !f.Current || f.CancelRequested {
		if !stopped {
			return AuthoredRegionCancelling, nil
		}
		if !f.Current {
			return AuthoredRegionStale, nil
		}
		return AuthoredRegionCancelled, nil
	}
	if f.CropCanonical {
		if !f.AllWritersClosed || !f.AllQuotaSettled {
			return AuthoredRegionCropping, nil
		}
		return AuthoredRegionReady, nil
	}
	if f.CropTerminal || f.RenderTerminal && !f.RenderCanonical {
		if !stopped {
			return AuthoredRegionCancelling, nil
		}
		return AuthoredRegionFailed, nil
	}
	if f.CropExists || f.RenderCanonical {
		return AuthoredRegionCropping, nil
	}
	if f.RenderExists {
		return AuthoredRegionRendering, nil
	}
	return AuthoredRegionAdmitted, nil
}
