package skoda

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/evcc-io/evcc/server/db/settings"
	"github.com/evcc-io/evcc/vehicle/skoda/pb"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
)

// testGetOrRegister is like GetOrRegisterToken but uses baseURL for all HTTP calls.
func (c *FCMClient) testGetOrRegister(ctx context.Context, baseURL string) (string, error) {
	// Try loading cached credentials
	var creds FCMCredentials
	if err := settings.Json(c.settingsKey, &creds); err == nil && creds.FCMToken != "" {
		c.log.DEBUG.Printf("fcm credentials loaded from db")

		// Validate by attempting checkin with existing credentials
		if _, _, err := c.testGCMCheckinWithID(ctx, baseURL, creds.AndroidID, creds.SecurityToken); err == nil {
			c.log.DEBUG.Printf("gcm checkin ok, android_id=%d", creds.AndroidID)
			return creds.FCMToken, nil
		}

		c.log.WARN.Printf("gcm checkin failed, re-registration required")
	}

	return c.testRegistration(ctx, baseURL)
}

// testRegistration performs the full 4-step registration using baseURL.
func (c *FCMClient) testRegistration(ctx context.Context, baseURL string) (string, error) {
	c.log.WARN.Printf("fcm re-registration required")

	androidID, securityToken, err := c.testGCMCheckin(ctx, baseURL)
	if err != nil {
		return "", fmt.Errorf("gcm checkin: %w", err)
	}
	c.log.DEBUG.Printf("gcm checkin ok, android_id=%d", androidID)

	gcmToken, err := c.testGCMRegister(ctx, baseURL, androidID, securityToken)
	if err != nil {
		return "", fmt.Errorf("gcm register: %w", err)
	}
	c.log.DEBUG.Printf("gcm register ok")

	fid, authToken, err := c.testFCMInstall(ctx, baseURL)
	if err != nil {
		return "", fmt.Errorf("fcm install: %w", err)
	}
	c.log.DEBUG.Printf("fcm install ok, fid=%s", fid)

	fcmToken, err := c.testFCMRegister(ctx, baseURL, gcmToken, fid, authToken)
	if err != nil {
		return "", fmt.Errorf("fcm register: %w", err)
	}
	c.log.INFO.Printf("fcm token obtained")

	creds := FCMCredentials{
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

func (c *FCMClient) testGCMCheckin(ctx context.Context, baseURL string) (uint64, uint64, error) {
	return c.testGCMCheckinWithID(ctx, baseURL, 0, 0)
}

func (c *FCMClient) testGCMCheckinWithID(ctx context.Context, baseURL string, androidID, securityToken uint64) (uint64, uint64, error) {
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

	if androidID != 0 {
		id := int64(androidID)
		req.Id = &id
		req.SecurityToken = &securityToken
	}

	body, err := proto.Marshal(req)
	if err != nil {
		return 0, 0, fmt.Errorf("marshal checkin request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/checkin", bytes.NewReader(body))
	if err != nil {
		return 0, 0, err
	}
	httpReq.Header.Set("Content-Type", "application/x-protobuf")

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

func (c *FCMClient) testGCMRegister(ctx context.Context, baseURL string, androidID, securityToken uint64) (string, error) {
	subtype := fmt.Sprintf("wp:%s#%s", firebaseAppID, uuid.New().String())

	form := url.Values{
		"app":       {"org.chromium.linux"},
		"X-subtype": {subtype},
		"device":    {fmt.Sprintf("%d", androidID)},
		"sender":    {fcmServerKey},
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/register3", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Authorization", fmt.Sprintf("AidLogin %d:%d", androidID, securityToken))
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

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

	for _, part := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(part, "token=") {
			return strings.TrimPrefix(part, "token="), nil
		}
	}

	return "", fmt.Errorf("gcm register: no token in response: %s", string(body))
}

func (c *FCMClient) testFCMInstall(ctx context.Context, baseURL string) (string, string, error) {
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

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/installations", bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")

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
	}

	if err := json.NewDecoder(resp.Body).Decode(&installResp); err != nil {
		return "", "", fmt.Errorf("decode fcm install response: %w", err)
	}

	return installResp.FID, installResp.AuthToken.Token, nil
}

func (c *FCMClient) testFCMRegister(ctx context.Context, baseURL, gcmToken, fid, authToken string) (string, error) {
	regReq := struct {
		Web struct {
			ApplicationPubKey string `json:"applicationPubKey"`
			Endpoint          string `json:"endpoint"`
			P256dh            string `json:"p256dh"`
			Auth              string `json:"auth"`
		} `json:"web"`
	}{}
	regReq.Web.ApplicationPubKey = fcmServerKey
	regReq.Web.Endpoint = fmt.Sprintf("%s/%s", baseURL, gcmToken)
	regReq.Web.P256dh = "test-p256dh"
	regReq.Web.Auth = "test-auth"

	body, err := json.Marshal(regReq)
	if err != nil {
		return "", err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/registrations", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
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
