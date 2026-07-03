package apiv1

import "testing"

func TestNormalizeDeadlineSecConvertsSupervisorEVMMilliseconds(t *testing.T) {
	const supervisorDeadlineMillis int64 = 1_783_682_106_000
	const wantDeadlineSeconds int64 = 1_783_682_106

	if got := normalizeDeadlineSec(supervisorDeadlineMillis); got != wantDeadlineSeconds {
		t.Fatalf("normalizeDeadlineSec(%d)=%d, want %d",
			supervisorDeadlineMillis, got, wantDeadlineSeconds)
	}
}

func TestNormalizeDeadlineSecPreservesStandardSeconds(t *testing.T) {
	const deadlineSeconds int64 = 1_783_682_106

	if got := normalizeDeadlineSec(deadlineSeconds); got != deadlineSeconds {
		t.Fatalf("normalizeDeadlineSec(%d)=%d, want unchanged",
			deadlineSeconds, got)
	}
}
