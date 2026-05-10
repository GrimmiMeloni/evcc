package skoda

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"time"
)

// GenerateTOTP generates a 6-digit TOTP code from the FCM token.
// This is a custom TOTP variant (not RFC 6238) using HMAC-SHA256.
func GenerateTOTP(fcmToken string) string {
	return generateTOTP(fcmToken, time.Now())
}

// generateTOTP is the internal implementation with injectable time for testing.
func generateTOTP(fcmToken string, now time.Time) string {
	// 1. HMAC key = SHA-256 hash of FCM token
	key := sha256.Sum256([]byte(fcmToken))

	// 2. Time step = floor(unix_timestamp / 30), big-endian uint64
	timeStep := make([]byte, 8)
	binary.BigEndian.PutUint64(timeStep, uint64(now.Unix()/30))

	// 3. HMAC-SHA256(key, time_step)
	mac := hmac.New(sha256.New, key[:])
	mac.Write(timeStep)
	hash := mac.Sum(nil)

	// 4. RFC 4226 dynamic truncation
	offset := hash[len(hash)-1] & 0x0F
	code := ((uint32(hash[offset]) & 0x7F) << 24) |
		((uint32(hash[offset+1]) & 0xFF) << 16) |
		((uint32(hash[offset+2]) & 0xFF) << 8) |
		(uint32(hash[offset+3]) & 0xFF)

	// 5. Modulo 10^6, zero-padded to 6 digits
	return fmt.Sprintf("%06d", code%1_000_000)
}
