package main

import "testing"

func TestAdmissionSchedulerCredentialsRemainSeparate(t *testing.T) {
	const api = "postgres://api-login@localhost/photo"
	const scheduler = "postgres://admission-login@localhost/photo"
	t.Setenv("PHOTO_ADMISSION_SCHEDULER_DATABASE_URL", scheduler)
	if got := admissionSchedulerDatabaseURL(api); got != scheduler {
		t.Fatal("dedicated scheduler credentials ignored")
	}
	t.Setenv("PHOTO_ADMISSION_SCHEDULER_DATABASE_URL", "")
	if got := admissionSchedulerDatabaseURL(api); got != api {
		t.Fatal("legacy deployment fallback changed")
	}
}
