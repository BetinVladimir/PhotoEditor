package main

import (
 "context"
 "errors"
 "testing"
)

func TestAdvancedTrainingRequiresExplicitSchedulerConfiguration(t *testing.T) {
 // Missing optional installation must not try API/libpq defaults or require
 // live dependencies; baseline style analysis has its separate constructor.
 service, closeService, err := buildTrainingAdmission(context.Background(), nil, "", nil, "")
 if err != nil || service != nil || closeService == nil { t.Fatal("unconfigured advanced training was activated", err) }
 closeService()
 if _, _, err = buildTrainingAdmission(context.Background(), nil, "postgresql://scheduler.invalid/photo", nil, ""); err == nil {
  t.Fatal("partial explicit installation accepted")
 }
 ctx, cancel := context.WithCancel(context.Background()); cancel()
 if _, _, err = buildTrainingAdmission(ctx, nil, "", nil, ""); !errors.Is(err, context.Canceled) {
  t.Fatal("cancelled startup accepted", err)
 }
}
