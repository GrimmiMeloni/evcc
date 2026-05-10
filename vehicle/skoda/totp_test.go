package skoda

import (
	"testing"
	"time"
)

func TestGenerateTOTP_KnownVector(t *testing.T) {
	// Known test vector: fixed token + fixed time = expected code
	fcmToken := "test-fcm-token-12345"
	fixedTime := time.Unix(1704067200, 0) // 2024-01-01 00:00:00 UTC

	got := generateTOTP(fcmToken, fixedTime)
	want := "733334"

	if got != want {
		t.Errorf("generateTOTP(%q, %v) = %q, want %q", fcmToken, fixedTime, got, want)
	}
}

func TestGenerateTOTP_Boundary(t *testing.T) {
	fcmToken := "test-fcm-token-12345"

	// t=29 and t=30 are in different 30-second windows (step 0 vs step 1)
	code29 := generateTOTP(fcmToken, time.Unix(29, 0))
	code30 := generateTOTP(fcmToken, time.Unix(30, 0))
	code31 := generateTOTP(fcmToken, time.Unix(31, 0))

	// t=29 (step=0) should differ from t=30 (step=1)
	if code29 == code30 {
		t.Errorf("expected different codes at boundary: t=29 got %q, t=30 got %q", code29, code30)
	}

	// t=30 and t=31 are in the same window (step=1)
	if code30 != code31 {
		t.Errorf("expected same code within window: t=30 got %q, t=31 got %q", code30, code31)
	}

	// Verify exact values
	if code29 != "466171" {
		t.Errorf("t=29: got %q, want %q", code29, "466171")
	}
	if code30 != "451998" {
		t.Errorf("t=30: got %q, want %q", code30, "451998")
	}
}

func TestGenerateTOTP_Deterministic(t *testing.T) {
	fcmToken := "determinism-test-token"
	fixedTime := time.Unix(1700000000, 0)

	first := generateTOTP(fcmToken, fixedTime)
	for i := 0; i < 100; i++ {
		got := generateTOTP(fcmToken, fixedTime)
		if got != first {
			t.Fatalf("non-deterministic: iteration %d got %q, expected %q", i, got, first)
		}
	}
}

func TestGenerateTOTP_SixDigits(t *testing.T) {
	// Verify output is always 6 digits (zero-padded)
	fcmToken := "padding-test-token"
	for i := int64(0); i < 100; i++ {
		code := generateTOTP(fcmToken, time.Unix(i*30, 0))
		if len(code) != 6 {
			t.Errorf("t=%d: got %q (len=%d), expected 6 digits", i*30, code, len(code))
		}
	}
}
