package skoda

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/evcc-io/evcc/server/db/settings"
	"github.com/evcc-io/evcc/util"
	"github.com/evcc-io/evcc/vehicle/skoda/pb"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
)

const (
	firebaseProjectID      = "678067506455"
	firebaseAppID          = "1:678067506455:android:4afca86c91d6d4c235bb52"
	firebaseAPIKey         = "AIzaSyBlJdDfVR6ltRhKpA87F3SmCe2hHqhyEd8"
	firebaseAndroidPackage = "cz.skodaauto.myskoda"
	firebaseAndroidCert    = "E567A2E2E6C5E889CDB37EF07EBEC1576C196325"
	myskodaAppVersion      = "8.11.0"

	gcmCheckinURL      = "https://android.clients.google.com/checkin"
	gcmRegisterURL     = "https://android.clients.google.com/c2dm/register3"
	fcmInstallURL      = "https://firebaseinstallations.googleapis.com/v1/"
	fcmRegistrationURL = "https://fcmregistrations.googleapis.com/v1/"
	fcmSendURL         = "https://fcm.googleapis.com/fcm/send/"
	fcmServerKey       = "BDOU99-h67HcA6JeFXHbSNMu7e2yNNu3RzoMj8TM4W88jITfq7ZmPvIM1Iv-4_l2LxQcYwhqby2xGpWwzjfAnG4"
	fcmAuthVersion     = "FIS_v2"
	fcmSDKVersion      = "w:0.6.17"
	gcmChromeVersion   = "94.0.4606.51"
)

// FCMCredentials contains all persisted FCM registration data.
type FCMCredentials struct {
	AndroidID     uint64 `json:"androidId"`
	SecurityToken uint64 `json:"securityToken"`
	GCMToken      string `json:"gcmToken"`
	FCMToken      string `json:"fcmToken"`
}

// FCMClient handles Firebase Cloud Messaging token registration.
type FCMClient struct {
	log         *util.Logger
	httpClient  *http.Client
	settingsKey string
}

// NewFCMClient creates a new FCM client for token acquisition.
// Uses a plain http.Client (not OAuth-authenticated) for Google API calls.
func NewFCMClient(log *util.Logger, httpClient *http.Client, username string) *FCMClient {
	return &FCMClient{
		log:         log,
		httpClient:  httpClient,
		settingsKey: fmt.Sprintf("skoda.fcm.%s", username),
	}
}

// GetOrRegisterToken returns an FCM token, loading from DB or performing fresh registration.
func (c *FCMClient) GetOrRegisterToken(ctx context.Context) (string, error) {
	// Try loading cached credentials
	var creds FCMCredentials
	if err := settings.Json(c.settingsKey, &creds); err == nil && creds.FCMToken != "" {
		c.log.DEBUG.Printf("fcm credentials loaded from db")

		// Validate by attempting checkin with existing credentials
		if _, _, err := c.gcmCheckinWithID(ctx, creds.AndroidID, creds.SecurityToken); err == nil {
			c.log.DEBUG.Printf("gcm checkin ok, android_id=%d", creds.AndroidID)
			return creds.FCMToken, nil
		}

		c.log.WARN.Printf("gcm checkin failed, re-registration required")
	}

	// Full 4-step registration
	c.log.WARN.Printf("fcm re-registration required")

	// Step 1: GCM Checkin
	androidID, securityToken, err := c.gcmCheckin(ctx)
	if err != nil {
		return "", fmt.Errorf("gcm checkin: %w", err)
	}
	c.log.DEBUG.Printf("gcm checkin ok, android_id=%d", androidID)

	// Step 2: GCM Register
	gcmToken, err := c.gcmRegister(ctx, androidID, securityToken)
	if err != nil {
		return "", fmt.Errorf("gcm register: %w", err)
	}
	c.log.DEBUG.Printf("gcm register ok")

	// Step 3: FCM Install
	fid, authToken, err := c.fcmInstall(ctx)
	if err != nil {
		return "", fmt.Errorf("fcm install: %w", err)
	}
	c.log.DEBUG.Printf("fcm install ok, fid=%s", fid)

	// Step 4: FCM Register
	fcmToken, err := c.fcmRegister(ctx, gcmToken, authToken)
	if err != nil {
		return "", fmt.Errorf("fcm register: %w", err)
	}
	c.log.INFO.Printf("fcm token obtained")

	// Persist credentials
	creds = FCMCredentials{
		AndroidID:     androidID,
		SecurityToken: securityToken,
		GCMToken:      gcmToken,
		FCMToken:      fcmToken,
	}
	if err := settings.SetJson(c.settingsKey, creds); err != nil {
		return "", fmt.Errorf("persist fcm credentials: %w", err)
	}
	c.log.DEBUG.Printf("fcm credentials saved to db")

	return fcmToken, nil
}

// gcmCheckin performs a fresh GCM checkin without existing credentials.
func (c *FCMClient) gcmCheckin(ctx context.Context) (uint64, uint64, error) {
	return c.gcmCheckinWithID(ctx, 0, 0)
}

// gcmCheckinWithID performs a GCM checkin, optionally with existing android_id and security_token.
func (c *FCMClient) gcmCheckinWithID(ctx context.Context, androidID, securityToken uint64) (uint64, uint64, error) {
	chromeVersion := gcmChromeVersion
	platform := int32(2)   // PLATFORM_LINUX
	channel := int32(1)    // CHANNEL_STABLE
	deviceType := int32(3) // DEVICE_CHROME_BROWSER
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

	if androidID != 0 {
		id := int64(androidID)
		req.Id = &id
		req.SecurityToken = &securityToken
	}

	body, err := proto.Marshal(req)
	if err != nil {
		return 0, 0, fmt.Errorf("marshal checkin request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, gcmCheckinURL, bytes.NewReader(body))
	if err != nil {
		return 0, 0, err
	}
	httpReq.Header.Set("Content-Type", "application/x-protobuf")
	httpReq.Header.Set("X-Android-Package", firebaseAndroidPackage)
	httpReq.Header.Set("X-Android-Cert", firebaseAndroidCert)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, 0, fmt.Errorf("checkin returned status %d", resp.StatusCode)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, 0, err
	}

	var checkinResp pb.AndroidCheckinResponse
	if err := proto.Unmarshal(respBody, &checkinResp); err != nil {
		return 0, 0, fmt.Errorf("unmarshal checkin response: %w", err)
	}

	return checkinResp.GetAndroidId(), checkinResp.GetSecurityToken(), nil
}

// gcmRegister registers with GCM to obtain a GCM token.
func (c *FCMClient) gcmRegister(ctx context.Context, androidID, securityToken uint64) (string, error) {
	subtype := fmt.Sprintf("wp:receiver.push.com#%s", uuid.New().String())

	form := url.Values{
		"app":       {"org.chromium.linux"},
		"X-subtype": {subtype},
		"device":    {fmt.Sprintf("%d", androidID)},
		"sender":    {fcmServerKey},
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, gcmRegisterURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Authorization", fmt.Sprintf("AidLogin %d:%d", androidID, securityToken))
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpReq.Header.Set("X-Android-Package", firebaseAndroidPackage)
	httpReq.Header.Set("X-Android-Cert", firebaseAndroidCert)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gcm register returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// Response format: "token=<TOKEN>" or may contain other key=value pairs
	for _, part := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(part, "token=") {
			return strings.TrimPrefix(part, "token="), nil
		}
	}

	return "", fmt.Errorf("gcm register: no token in response: %s", string(body))
}

// fcmInstall creates a Firebase installation to obtain FID and auth token.
func (c *FCMClient) fcmInstall(ctx context.Context) (string, string, error) {
	fid := generateFID()

	installReq := struct {
		AppID       string `json:"appId"`
		AuthVersion string `json:"authVersion"`
		FID         string `json:"fid"`
		SDKVersion  string `json:"sdkVersion"`
	}{
		AppID:       firebaseAppID,
		AuthVersion: fcmAuthVersion,
		FID:         fid,
		SDKVersion:  fcmSDKVersion,
	}

	body, err := json.Marshal(installReq)
	if err != nil {
		return "", "", err
	}

	uri := fmt.Sprintf("%sprojects/%s/installations", fcmInstallURL, firebaseProjectID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, uri, bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", firebaseAPIKey)
	httpReq.Header.Set("X-Android-Package", firebaseAndroidPackage)
	httpReq.Header.Set("X-Android-Cert", firebaseAndroidCert)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("fcm install returned status %d", resp.StatusCode)
	}

	var installResp struct {
		FID       string `json:"fid"`
		AuthToken struct {
			Token     string `json:"token"`
			ExpiresIn string `json:"expiresIn"`
		} `json:"authToken"`
		RefreshToken string `json:"refreshToken"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&installResp); err != nil {
		return "", "", fmt.Errorf("decode fcm install response: %w", err)
	}

	return installResp.FID, installResp.AuthToken.Token, nil
}

// fcmRegister registers with FCM to obtain the final FCM token.
func (c *FCMClient) fcmRegister(ctx context.Context, gcmToken, authToken string) (string, error) {
	// Generate ECDH P-256 key pair (required by API but not used afterward)
	curve := ecdh.P256()
	privKey, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return "", fmt.Errorf("generate ecdh key: %w", err)
	}
	pubKeyBytes := privKey.PublicKey().Bytes()
	p256dh := base64.RawURLEncoding.EncodeToString(pubKeyBytes)

	// Generate 16-byte auth secret
	authSecret := make([]byte, 16)
	if _, err := rand.Read(authSecret); err != nil {
		return "", fmt.Errorf("generate auth secret: %w", err)
	}
	authSecretB64 := base64.RawURLEncoding.EncodeToString(authSecret)

	regReq := struct {
		Web struct {
			ApplicationPubKey string `json:"applicationPubKey"`
			Endpoint          string `json:"endpoint"`
			P256dh            string `json:"p256dh"`
			Auth              string `json:"auth"`
		} `json:"web"`
	}{}
	regReq.Web.ApplicationPubKey = fcmServerKey
	regReq.Web.Endpoint = fmt.Sprintf("%s%s", fcmSendURL, gcmToken)
	regReq.Web.P256dh = p256dh
	regReq.Web.Auth = authSecretB64

	body, err := json.Marshal(regReq)
	if err != nil {
		return "", err
	}

	uri := fmt.Sprintf("%sprojects/%s/registrations", fcmRegistrationURL, firebaseProjectID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, uri, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", firebaseAPIKey)
	httpReq.Header.Set("X-Android-Package", firebaseAndroidPackage)
	httpReq.Header.Set("X-Android-Cert", firebaseAndroidCert)
	httpReq.Header.Set("x-goog-firebase-installations-auth", fmt.Sprintf("FIS %s", authToken))

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fcm register returned status %d", resp.StatusCode)
	}

	var regResp struct {
		Token   string `json:"token"`
		PushSet string `json:"pushSet"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&regResp); err != nil {
		return "", fmt.Errorf("decode fcm register response: %w", err)
	}

	if regResp.Token == "" {
		return "", fmt.Errorf("fcm register: empty token in response")
	}

	return regResp.Token, nil
}

// generateFID generates a Firebase Installation ID.
// 17 random bytes, base64url-encoded (22 chars), first 4 bits set to 0111.
func generateFID() string {
	b := make([]byte, 17)
	_, _ = rand.Read(b)
	// Set first 4 bits to 0111 per FID spec
	b[0] = 0x70 | (b[0] & 0x0F)
	return base64.RawURLEncoding.EncodeToString(b)[:22]
}
