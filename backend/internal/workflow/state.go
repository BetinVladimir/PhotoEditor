package workflow

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"github.com/google/uuid"
	"time"
)

type TaskState string

const (
	Blocked   TaskState = "blocked"
	Ready     TaskState = "ready"
	Queued    TaskState = "queued"
	Running   TaskState = "running"
	Succeeded TaskState = "succeeded"
	RetryWait TaskState = "retry_wait"
	Failed    TaskState = "failed"
	Cancelled TaskState = "cancelled"
	Skipped   TaskState = "skipped"
)

func (s TaskState) Terminal() bool {
	return s == Succeeded || s == Failed || s == Cancelled || s == Skipped
}
func CanTransition(from, to TaskState) bool {
	switch from {
	case Blocked:
		return to == Ready || to == Skipped || to == Cancelled
	case Ready:
		return to == Queued || to == Cancelled || to == Skipped
	case Queued:
		return to == Running || to == Cancelled
	case Running:
		return to == Succeeded || to == RetryWait || to == Failed || to == Cancelled
	case RetryWait:
		return to == Ready || to == Failed || to == Cancelled
	}
	return false
}

type Failure struct {
	Code      string
	Retryable bool
}
type RetryContext struct {
	TaskID                        uuid.UUID
	Attempt, RetryCount, OOMCount int
	CancelRequested               bool
	CanReduce                     bool
	CanFallbackCPU                bool
	Now                           time.Time
}
type RetryDecision struct {
	State                        TaskState
	RetryCount, OOMCount         int
	NextAttemptAt                *time.Time
	ReduceResources, FallbackCPU bool
	Reason                       string
}

// DecideRetry counts claimed execution attempts, never duplicate broker deliveries.
func DecideRetry(c RetryContext, e Failure) RetryDecision {
	d := RetryDecision{State: Failed, RetryCount: c.RetryCount, OOMCount: c.OOMCount, Reason: e.Code}
	if c.CancelRequested {
		d.State = Cancelled
		d.Reason = "cancel_requested"
		return d
	}
	if c.Attempt < 1 || c.Attempt >= 6 || c.RetryCount < 0 || c.RetryCount > 3 || c.OOMCount < 0 || c.OOMCount > 2 {
		return d
	}
	if e.Code == "RESOURCE_OOM" {
		if c.OOMCount < 2 && c.CanReduce {
			d.OOMCount++
			d.ReduceResources = true
		} else if c.CanFallbackCPU && c.RetryCount < 3 {
			d.RetryCount++
			d.FallbackCPU = true
		} else {
			return d
		}
	} else {
		if !e.Retryable || c.RetryCount >= 3 {
			return d
		}
		d.RetryCount++
	}
	delays := []time.Duration{5 * time.Second, 30 * time.Second, 120 * time.Second}
	index := d.RetryCount - 1
	if d.ReduceResources {
		index = d.OOMCount - 1
	}
	if index < 0 {
		index = 0
	}
	if index > 2 {
		index = 2
	}
	base := delays[index]
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%s", c.TaskID, c.Attempt, e.Code)))
	u := float64(binary.BigEndian.Uint64(hash[:8])>>11) / float64(uint64(1)<<53)
	delay := time.Duration(float64(base) * (0.8 + 0.4*u))
	next := c.Now.Add(delay)
	d.State = RetryWait
	d.NextAttemptAt = &next
	return d
}

type RunSummary struct {
	GraphSealed                                                                             bool
	OpenExpansions, PendingReviews, ActiveTasks, AcceptedOutputs, FailedItems, SkippedItems int
	CancelRequested                                                                         bool
}

func AggregateRun(s RunSummary) string {
	if s.CancelRequested {
		if s.ActiveTasks > 0 {
			return "cancelling"
		}
		return "cancelled"
	}
	if s.PendingReviews > 0 {
		return "waiting_review"
	}
	if !s.GraphSealed || s.OpenExpansions > 0 {
		return "running"
	}
	if s.ActiveTasks > 0 {
		return "running"
	}
	if s.FailedItems+s.SkippedItems > 0 {
		if s.AcceptedOutputs > 0 {
			return "completed_with_errors"
		}
		return "failed"
	}
	return "completed"
}

type Dependency struct {
	State      TaskState
	SkipReason string
	Condition  string
	Decision   string
}

// DependencyStatus returns wait/allow/reject. Disabled pass-through is successful;
// upstream failure cannot silently become an identity image.
func DependencyStatus(d Dependency) string {
	if !d.State.Terminal() {
		return "wait"
	}
	if d.Condition == "terminal" {
		return "allow"
	}
	success := d.State == Succeeded || d.State == Skipped && d.SkipReason == "disabled"
	if !success {
		return "reject"
	}
	switch d.Condition {
	case "success":
		return "allow"
	case "accepted":
		if d.Decision == "" {
			return "wait"
		}
		if d.Decision == "accept" {
			return "allow"
		}
	case "rejected":
		if d.Decision == "" {
			return "wait"
		}
		if d.Decision == "reject" {
			return "allow"
		}
	}
	return "reject"
}
