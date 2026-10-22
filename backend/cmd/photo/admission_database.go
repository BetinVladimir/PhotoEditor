package main

import "os"

// Admission builders use their own scheduler-role pool. Explicit credentials
// allow the API login to remain a member only of its API/Portal roles. The
// fallback preserves existing deployments that supplied a multi-role login.
func admissionSchedulerDatabaseURL(legacy string) string {
	if dedicated := os.Getenv("PHOTO_ADMISSION_SCHEDULER_DATABASE_URL"); dedicated != "" {
		return dedicated
	}
	return legacy
}
