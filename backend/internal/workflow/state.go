package workflow

import "photoeditor/backend/internal/taskrecovery"

type TaskState = taskrecovery.TaskState
type Failure = taskrecovery.Failure
type RetryContext = taskrecovery.RetryContext
type RetryDecision = taskrecovery.RetryDecision
type RunSummary = taskrecovery.RunSummary
type Dependency = taskrecovery.Dependency

const Blocked = taskrecovery.Blocked
const Ready = taskrecovery.Ready
const Queued = taskrecovery.Queued
const Running = taskrecovery.Running
const Succeeded = taskrecovery.Succeeded
const RetryWait = taskrecovery.RetryWait
const Failed = taskrecovery.Failed
const Cancelled = taskrecovery.Cancelled
const Skipped = taskrecovery.Skipped

func CanTransition(from, to TaskState) bool               { return taskrecovery.CanTransition(from, to) }
func DecideRetry(c RetryContext, e Failure) RetryDecision { return taskrecovery.DecideRetry(c, e) }
func AggregateRun(s RunSummary) string                    { return taskrecovery.AggregateRun(s) }
func DependencyStatus(d Dependency) string                { return taskrecovery.DependencyStatus(d) }
