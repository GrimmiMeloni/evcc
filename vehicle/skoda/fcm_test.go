package skoda

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/evcc-io/evcc/server/db/settings"
	"github.com/evcc-io/evcc/util"
	"github.com/evcc-io/evcc/vehicle/skoda/pb"
	"google.golang.org/protobuf/proto"
)

func TestFCMClient_HappyPath(t *testing.T) {
	// Build a mock server that handles all 4 registration steps
	var checkinCalls, registerCalls, installCalls, fcmRegCalls int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/checkin"):
			checkinCalls++
			androidID := uint64(123456789)
			secToken := uint64(987654321)
			resp := &pb.AndroidCheckinResponse{
				AndroidId:     &androidID,
				SecurityToken: &secToken,
			}
			b, _ := proto.Marshal(resp)
			w.Header().Set("Content-Type", "application/x-protobuf")
			w.Write(b)

		case strings.HasSuffix(r.URL.Path, "/register3"):
			registerCalls++
			fmt.Fprint(w, "token=mock-gcm-token-abc123")

		case strings.Contains(r.URL.Path, "/installations"):
			installCalls++
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"fid":"mock-fid-123","authToken":{"token":"mock-auth-token","expiresIn":"604800s"},"refreshToken":"mock-refresh"}`)

		case strings.Contains(r.URL.Path, "/registrations"):
			fcmRegCalls++
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"token":"mock-fcm-token-final","pushSet":"mock-push-set"}`)

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	// Create client with test URLs pointing to mock server
	log := util.NewLogger("test")
	client := &FCMClient{
		log:         log,
		httpClient:  srv.Client(),
		settingsKey: "test.fcm.happypath",
	}

	// Override URLs to point to test server
	token, err := client.testRegistration(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("GetOrRegisterToken failed: %v", err)
	}

	if token != "mock-fcm-token-final" {
		t.Errorf("got token %q, want %q", token, "mock-fcm-token-final")
	}

	if checkinCalls != 1 {
		t.Errorf("expected 1 checkin call, got %d", checkinCalls)
	}
	if registerCalls != 1 {
		t.Errorf("expected 1 register call, got %d", registerCalls)
	}
	if installCalls != 1 {
		t.Errorf("expected 1 install call, got %d", installCalls)
	}
	if fcmRegCalls != 1 {
		t.Errorf("expected 1 fcm register call, got %d", fcmRegCalls)
	}
}

func TestFCMClient_CachedToken(t *testing.T) {
	// Pre-populate settings with cached credentials
	key := "test.fcm.cached"
	creds := FCMCredentials{
		AndroidID:     111,
		SecurityToken: 222,
		GCMToken:      "cached-gcm",
		FCMToken:      "cached-fcm-token",
	}
	if err := settings.SetJson(key, creds); err != nil {
		t.Fatalf("SetJson failed: %v", err)
	}

	var httpCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpCalls++
		// Checkin succeeds for cached credentials
		if strings.HasSuffix(r.URL.Path, "/checkin") {
			androidID := uint64(111)
			secToken := uint64(222)
			resp := &pb.AndroidCheckinResponse{
				AndroidId:     &androidID,
				SecurityToken: &secToken,
			}
			b, _ := proto.Marshal(resp)
			w.Header().Set("Content-Type", "application/x-protobuf")
			w.Write(b)
			return
		}
		t.Errorf("unexpected request to %s (should use cached token)", r.URL.Path)
	}))
	defer srv.Close()

	log := util.NewLogger("test")
	client := &FCMClient{
		log:         log,
		httpClient:  srv.Client(),
		settingsKey: key,
	}

	token, err := client.testGetOrRegister(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("GetOrRegisterToken failed: %v", err)
	}

	if token != "cached-fcm-token" {
		t.Errorf("got token %q, want %q", token, "cached-fcm-token")
	}

	// Only the checkin call should have been made (to validate credentials)
	if httpCalls != 1 {
		t.Errorf("expected 1 HTTP call (checkin only), got %d", httpCalls)
	}
}

func TestFCMClient_CheckinRetry(t *testing.T) {
	// Pre-populate with cached credentials
	key := "test.fcm.retry"
	creds := FCMCredentials{
		AndroidID:     333,
		SecurityToken: 444,
		GCMToken:      "old-gcm",
		FCMToken:      "old-fcm-token",
	}
	if err := settings.SetJson(key, creds); err != nil {
		t.Fatalf("SetJson failed: %v", err)
	}

	checkinCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/checkin"):
			checkinCalls++
			if checkinCalls == 1 {
				// First checkin fails (cached creds invalid)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			// Second checkin succeeds (fresh registration)
			androidID := uint64(555)
			secToken := uint64(666)
			resp := &pb.AndroidCheckinResponse{
				AndroidId:     &androidID,
				SecurityToken: &secToken,
			}
			b, _ := proto.Marshal(resp)
			w.Header().Set("Content-Type", "application/x-protobuf")
			w.Write(b)

		case strings.HasSuffix(r.URL.Path, "/register3"):
			fmt.Fprint(w, "token=new-gcm-token")

		case strings.Contains(r.URL.Path, "/installations"):
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"fid":"new-fid","authToken":{"token":"new-auth","expiresIn":"604800s"},"refreshToken":"new-refresh"}`)

		case strings.Contains(r.URL.Path, "/registrations"):
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"token":"new-fcm-token","pushSet":"new-push-set"}`)

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	log := util.NewLogger("test")
	client := &FCMClient{
		log:         log,
		httpClient:  srv.Client(),
		settingsKey: key,
	}

	token, err := client.testGetOrRegister(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("GetOrRegisterToken failed: %v", err)
	}

	if token != "new-fcm-token" {
		t.Errorf("got token %q, want %q", token, "new-fcm-token")
	}

	// Should have 2 checkin calls: first failed, second succeeded during re-registration
	if checkinCalls != 2 {
		t.Errorf("expected 2 checkin calls, got %d", checkinCalls)
	}
}

func TestProtobufRoundTrip(t *testing.T) {
	chromeVersion := gcmChromeVersion
	platform := int32(2)
	channel := int32(1)
	deviceType := int32(3)
	serialNumber := int32(0)
	version := int32(3)

	req := &pb.AndroidCheckinRequest{
		UserSerialNumber: &serialNumber,
		Checkin: &pb.AndroidCheckinProto{
			Type: &deviceType,
			ChromeBuild: &pb.ChromeBuildProto{
				Platform:      &platform,
				ChromeVersion: &chromeVersion,
				Channel:       &channel,
			},
		},
		Version: &version,
	}

	// Marshal
	data, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	if len(data) == 0 {
		t.Fatal("marshaled data is empty")
	}

	// Unmarshal
	var decoded pb.AndroidCheckinRequest
	if err := proto.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.GetVersion() != 3 {
		t.Errorf("version: got %d, want 3", decoded.GetVersion())
	}
	if decoded.GetUserSerialNumber() != 0 {
		t.Errorf("serial: got %d, want 0", decoded.GetUserSerialNumber())
	}
	if decoded.GetCheckin().GetType() != 3 {
		t.Errorf("type: got %d, want 3", decoded.GetCheckin().GetType())
	}
	if decoded.GetCheckin().GetChromeBuild().GetPlatform() != 2 {
		t.Errorf("platform: got %d, want 2", decoded.GetCheckin().GetChromeBuild().GetPlatform())
	}
	if decoded.GetCheckin().GetChromeBuild().GetChromeVersion() != gcmChromeVersion {
		t.Errorf("chrome_version: got %q, want %q", decoded.GetCheckin().GetChromeBuild().GetChromeVersion(), gcmChromeVersion)
	}
	if decoded.GetCheckin().GetChromeBuild().GetChannel() != 1 {
		t.Errorf("channel: got %d, want 1", decoded.GetCheckin().GetChromeBuild().GetChannel())
	}

	// Test with android_id and security_token
	androidID := int64(12345)
	secToken := uint64(67890)
	req.Id = &androidID
	req.SecurityToken = &secToken

	data2, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("marshal with id failed: %v", err)
	}

	var decoded2 pb.AndroidCheckinRequest
	if err := proto.Unmarshal(data2, &decoded2); err != nil {
		t.Fatalf("unmarshal with id failed: %v", err)
	}

	if decoded2.GetId() != 12345 {
		t.Errorf("id: got %d, want 12345", decoded2.GetId())
	}
	if decoded2.GetSecurityToken() != 67890 {
		t.Errorf("security_token: got %d, want 67890", decoded2.GetSecurityToken())
	}
}

func TestGenerateFID(t *testing.T) {
	fid := generateFID()

	// FID should be 22 chars (17 bytes base64url-encoded)
	if len(fid) != 22 {
		t.Errorf("fid length: got %d, want 22", len(fid))
	}

	// First 4 bits should be 0111
	// Decode back to check
	fid2 := generateFID()
	if fid == fid2 {
		t.Error("two FIDs should not be identical (randomness)")
	}
}
