package semanticproposal

import "os"

func nativeQualifiedFixtureBackend() string {
	value := os.Getenv("PHOTO_TEST_NATIVE_QUALIFIED_BACKEND")
	if value == "" {
		return "cuda"
	}
	if value != "cuda" && value != "mps" {
		panic("NATIVE_FIXTURE_BACKEND_INVALID")
	}
	return value
}
