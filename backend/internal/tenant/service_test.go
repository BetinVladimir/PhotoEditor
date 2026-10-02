package tenant

import "testing"

func TestTenantLimits(t *testing.T) {
	if !validQuotas(defaultQuotas) || !validRetention(defaultRetention) {
		t.Fatal("default tenant limits must be valid")
	}
	for _, value := range []Quotas{
		{StorageBytes: 1024*1024 - 1, SourceFiles: 1, MaxActiveGPUTasks: 1},
		{StorageBytes: 1 << 20, SourceFiles: 0, MaxActiveGPUTasks: 1},
		{StorageBytes: 1 << 20, SourceFiles: 1, MaxActiveGPUTasks: 17},
	} {
		if validQuotas(value) {
			t.Fatalf("invalid quotas accepted: %+v", value)
		}
	}
	for _, value := range []Retention{
		{ProcessingDays: 0, ExportsDays: 1, TemporaryHours: 1},
		{ProcessingDays: 1, ExportsDays: 366, TemporaryHours: 1},
		{ProcessingDays: 1, ExportsDays: 1, TemporaryHours: 169},
	} {
		if validRetention(value) {
			t.Fatalf("invalid retention accepted: %+v", value)
		}
	}
}
