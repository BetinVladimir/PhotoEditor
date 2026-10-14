package workflow

import (
 "context"
 "sync"
 "time"

 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)

type ReplacementDetectionAdoptionCandidates interface {
 Next(context.Context, uuid.UUID) (*ReplacementDetectionAdoptionCandidate, error)
}
type ReplacementDetectionAdoptionSourceLoader interface {
 Load(context.Context, ReplacementDetectionAdoptionCandidate) (ReplacementDetectionDocuments, error)
}
type ReplacementDetectionAdoptionRunner interface {
 Run(context.Context, ReplacementDetectionDocuments, int, int64, uuid.UUID, v2input.Ref) (bool, error)
}

// ProcessOne reconstructs current physical authority before invoking the pinned
// replay runner. A failed job advances the cursor; the next empty page wraps it.
// Other scheduler instances may repeat work; durable adoption owns idempotency.
type ReplacementDetectionAdoptionDispatcher struct {
 Candidates ReplacementDetectionAdoptionCandidates
 Loader ReplacementDetectionAdoptionSourceLoader
 Runner ReplacementDetectionAdoptionRunner
 Timeout time.Duration
 mu sync.Mutex
 after uuid.UUID
}

func (d *ReplacementDetectionAdoptionDispatcher) ProcessOne(ctx context.Context) (bool, error) {
 if err := ctx.Err(); err != nil { return false, err }
 if d == nil { return false, v2input.ErrInvalid }
 // A loop tick must not queue behind a long-running replay.
 if !d.mu.TryLock() { return false, nil }
 defer d.mu.Unlock()
 budget := d.Timeout
 if budget == 0 { budget = 10*time.Minute }
 if d.Candidates == nil || d.Loader == nil || d.Runner == nil || budget < time.Second || budget > 10*time.Minute { return false, v2input.ErrInvalid }
 query, cancel := context.WithTimeout(ctx, 10*time.Second)
 candidate, err := d.Candidates.Next(query, d.after)
 cancel()
 if err != nil { return false, err }
 if candidate == nil { d.after = uuid.Nil; return false, nil }
 if candidate.Tenant == uuid.Nil || candidate.Run == uuid.Nil || candidate.Task == uuid.Nil || candidate.Snapshot == uuid.Nil || candidate.Result == uuid.Nil || candidate.Root.ArtifactID == uuid.Nil || domainDigestInvalid(candidate.Root.SHA256) || candidate.Attempt < 1 || candidate.Attempt > 6 || candidate.Fence < 1 || candidate.Fence > 9007199254740991 || bytesUUIDCompare(candidate.Run,d.after) <= 0 { return false,v2input.ErrInvalid }
 job := *candidate
 job.Descriptor = append([]byte(nil), candidate.Descriptor...)
 d.after = job.Run
 work, stop := context.WithTimeout(ctx,budget)
 defer stop()
 source, err := d.Loader.Load(work,job)
 if err != nil { return false,err }
 // Loader results must retain the exact discovery identity. The loader and
 // final commit independently restore the descriptor from physical proofs.
 descriptor, err := decodeReplacementDetectionDescriptor(job.Descriptor)
 if err != nil { return false,err }
 if descriptor.Tenant != job.Tenant || descriptor.Run != job.Run || descriptor.Task != job.Task || source.Snapshot != job.Snapshot || string(source.Plan.Descriptor()) != string(job.Descriptor) { return false,v2input.ErrInvalid }
 if err = work.Err(); err != nil { return false,err }
 _, err = d.Runner.Run(work,source,job.Attempt,job.Fence,job.Result,job.Root)
 if err != nil { return false,err }
 if err = work.Err(); err != nil { return false,err }
 return true,nil
}
