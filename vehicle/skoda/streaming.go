package skoda

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/eclipse/paho.golang/paho"
	"github.com/evcc-io/evcc/util"
	"github.com/google/uuid"
	"golang.org/x/oauth2"
)

const mqttBroker = "mqtt.messagehub.de:8883"

// MqttConnector manages MQTT streaming connections for Skoda vehicles.
// A single connector handles all VINs for a given userID.
type MqttConnector struct {
	mu            sync.RWMutex
	log           *util.Logger
	connected     bool
	subscriptions map[string]chan StreamingMessage // keyed by VIN
}

var (
	mqttMu          sync.Mutex
	mqttConnections = make(map[string]*MqttConnector) // keyed by userID
)

// NewMqttConnector returns an existing or creates a new connector for the userID.
func NewMqttConnector(log *util.Logger, userID, fcmToken string, ts oauth2.TokenSource) *MqttConnector {
	mqttMu.Lock()
	defer mqttMu.Unlock()

	if conn, ok := mqttConnections[userID]; ok {
		return conn
	}

	v := &MqttConnector{
		log:           log,
		subscriptions: make(map[string]chan StreamingMessage),
	}

	if !testing.Testing() {
		go v.run(userID, fcmToken, ts)
	}

	mqttConnections[userID] = v

	return v
}

// Subscribe registers a VIN for streaming messages and returns a receive channel.
func (c *MqttConnector) Subscribe(vin string) <-chan StreamingMessage {
	c.mu.Lock()
	defer c.mu.Unlock()

	ch := make(chan StreamingMessage, 1)
	c.subscriptions[vin] = ch

	return ch
}

// Connected returns whether the MQTT connection is active.
func (c *MqttConnector) Connected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.connected
}

func (c *MqttConnector) setConnected(connected bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.connected = connected
}

// run is the background goroutine that manages the MQTT connection with exponential backoff.
func (c *MqttConnector) run(userID, fcmToken string, ts oauth2.TokenSource) {
	bo := backoff.NewExponentialBackOff(
		backoff.WithInitialInterval(time.Second),
		backoff.WithMaxInterval(time.Minute),
		backoff.WithMaxElapsedTime(0),
	)

	for {
		time.Sleep(bo.NextBackOff())

		token, err := ts.Token()
		if err != nil {
			c.log.ERROR.Println("streaming token:", err)
			continue
		}

		totp := GenerateTOTP(fcmToken)

		if err := c.runMqtt(userID, token, totp); err != nil {
			c.log.ERROR.Println("streaming mqtt:", err)

			// don't reset backoff on auth errors
			if isAuthError(err) {
				continue
			}
		}

		bo.Reset()
	}
}

// runMqtt connects to the MQTT broker, subscribes to topics for all registered VINs,
// and listens for messages until the token expires or an error occurs.
func (c *MqttConnector) runMqtt(userID string, token *oauth2.Token, totp string) error {
	clientID := fmt.Sprintf("%s#%s", uuid.New().String(), uuid.New().String())

	c.log.DEBUG.Printf("connecting streaming mqtt (user: %s, expires: %v)", userID, token.Expiry.Round(time.Second))

	// TLS connection to broker
	conn, err := tls.DialWithDialer(
		&net.Dialer{Timeout: 30 * time.Second},
		"tcp",
		mqttBroker,
		&tls.Config{MinVersion: tls.VersionTLS12},
	)
	if err != nil {
		return fmt.Errorf("tls dial: %w", err)
	}

	ctx, cancel := context.WithDeadline(context.Background(), token.Expiry)
	defer cancel()

	// Create paho v5 client
	client := paho.NewClient(paho.ClientConfig{
		Conn:     conn,
		ClientID: clientID,
		OnPublishReceived: []func(paho.PublishReceived) (bool, error){
			func(pr paho.PublishReceived) (bool, error) {
				c.handler(pr.Packet.Topic, pr.Packet.Payload)
				return true, nil
			},
		},
		OnClientError: func(err error) {
			c.log.DEBUG.Printf("streaming client error: %v", err)
			cancel()
		},
	})

	// CONNECT with MQTTv5 enhanced auth
	cp := &paho.Connect{
		KeepAlive:    60,
		ClientID:     clientID,
		CleanStart:   true,
		Username:     userID,
		UsernameFlag: true,
		Password:     []byte(token.AccessToken),
		PasswordFlag: true,
		Properties: &paho.ConnectProperties{
			AuthMethod: "totp_v1",
			AuthData:   []byte(totp),
		},
	}

	connack, err := client.Connect(ctx, cp)
	if err != nil {
		conn.Close()
		return fmt.Errorf("mqtt connect: %w", err)
	}
	if connack.ReasonCode != 0 {
		conn.Close()
		return fmt.Errorf("mqtt connect rejected: reason=%d", connack.ReasonCode)
	}

	c.setConnected(true)
	defer c.setConnected(false)

	c.log.DEBUG.Println("streaming mqtt connected")

	// Subscribe to topics for all registered VINs
	if err := c.subscribeTopics(ctx, client, userID); err != nil {
		return fmt.Errorf("mqtt subscribe: %w", err)
	}

	// Wait until context expires (token expiry) or client disconnects
	<-ctx.Done()

	_ = client.Disconnect(&paho.Disconnect{ReasonCode: 0})

	return nil
}

// subscribeTopics subscribes to the 4 streaming topics for each registered VIN.
func (c *MqttConnector) subscribeTopics(ctx context.Context, client *paho.Client, userID string) error {
	c.mu.RLock()
	vins := make([]string, 0, len(c.subscriptions))
	for vin := range c.subscriptions {
		vins = append(vins, vin)
	}
	c.mu.RUnlock()

	suffixes := []string{
		"service-event/charging",
		"service-event/air-conditioning",
		"service-event/vehicle-status/odometer",
		"vehicle-event/vehicle-connection-status-update",
	}

	var subs []paho.SubscribeOptions
	for _, vin := range vins {
		for _, suffix := range suffixes {
			topic := fmt.Sprintf("%s/%s/%s", userID, vin, suffix)
			subs = append(subs, paho.SubscribeOptions{
				Topic: topic,
				QoS:   0,
			})
			c.log.DEBUG.Printf("subscribing to %s", topic)
		}
	}

	if len(subs) == 0 {
		return nil
	}

	_, err := client.Subscribe(ctx, &paho.Subscribe{
		Subscriptions: subs,
	})

	return err
}

// handler processes incoming MQTT messages and dispatches to VIN-specific channels.
func (c *MqttConnector) handler(topic string, payload []byte) {
	c.log.TRACE.Printf("recv streaming: %s %s", topic, string(payload))

	// Parse VIN from topic: {userID}/{vin}/...
	parts := strings.SplitN(topic, "/", 3)
	if len(parts) < 3 {
		c.log.ERROR.Printf("streaming: unexpected topic format: %s", topic)
		return
	}
	vin := parts[1]

	msg := StreamingMessage{
		Topic:   topic,
		Payload: payload,
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	if ch, ok := c.subscriptions[vin]; ok {
		// Non-blocking send — drain old message if full to ensure latest is available
		select {
		case ch <- msg:
		default:
			select {
			case <-ch:
			default:
			}
			ch <- msg
		}
	}
}

// isAuthError checks if an error is an authentication/authorization error.
func isAuthError(err error) bool {
	if err == nil {
		return false
	}

	s := err.Error()
	return strings.Contains(s, "rejected") ||
		strings.Contains(s, "not authorized") ||
		strings.Contains(s, "bad user") ||
		strings.Contains(s, "reason=134") || // Bad User Name or Password
		strings.Contains(s, "reason=135") // Not authorized
}

// UserIDFromToken extracts the subject (userID) from a JWT access token.
func UserIDFromToken(token *oauth2.Token) (string, error) {
	if token == nil || token.AccessToken == "" {
		return "", errors.New("empty access token")
	}

	// JWT is header.payload.signature — extract payload
	parts := strings.SplitN(token.AccessToken, ".", 3)
	if len(parts) != 3 {
		return "", errors.New("invalid JWT format")
	}

	// Base64url decode the payload (with padding)
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("decode JWT payload: %w", err)
	}

	var claims struct {
		Sub string `json:"sub"`
	}

	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", fmt.Errorf("unmarshal JWT claims: %w", err)
	}

	if claims.Sub == "" {
		return "", errors.New("empty subject in JWT")
	}

	return claims.Sub, nil
}
