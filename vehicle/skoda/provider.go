package skoda

import (
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/evcc-io/evcc/api"
	"github.com/evcc-io/evcc/util"
)

// Provider implements the vehicle api
type Provider struct {
	// REST getters (unchanged)
	statusG   func() (StatusResponse, error)
	chargerG  func() (ChargerResponse, error)
	settingsG func() (SettingsResponse, error)
	climateG  func() (ClimaterResponse, error)
	action    func(action, value string) error
	wakeup    func() error

	// streaming state
	connector     *MqttConnector
	mu            sync.RWMutex
	streaming     *StreamingData
	vehicleOnline bool
	resetStatusG  func() // resets statusG cache
	resetClimateG func() // resets climateG cache
}

// NewProvider creates a vehicle api provider (REST only, no streaming)
func NewProvider(api *API, vin string, cache time.Duration) *Provider {
	impl := &Provider{
		statusG: util.Cached(func() (StatusResponse, error) {
			return api.Status(vin)
		}, cache),
		chargerG: util.Cached(func() (ChargerResponse, error) {
			return api.Charger(vin)
		}, cache),
		climateG: util.Cached(func() (ClimaterResponse, error) {
			return api.Climater(vin)
		}, cache),
		settingsG: util.Cached(func() (SettingsResponse, error) {
			return api.Settings(vin)
		}, cache),
		action: func(action, value string) error {
			return api.Action(vin, action, value)
		},
		wakeup: func() error {
			return api.WakeUp(vin)
		},
	}
	return impl
}

// NewStreamingProvider creates a vehicle api provider with MQTT streaming support.
// When streaming data is available it takes precedence over REST API calls.
func NewStreamingProvider(log *util.Logger, api *API, vin string, cache time.Duration, connector *MqttConnector) *Provider {
	statusCache := util.ResettableCached(func() (StatusResponse, error) {
		return api.Status(vin)
	}, cache)

	climateCache := util.ResettableCached(func() (ClimaterResponse, error) {
		return api.Climater(vin)
	}, cache)

	impl := &Provider{
		statusG: statusCache.Get,
		chargerG: util.Cached(func() (ChargerResponse, error) {
			return api.Charger(vin)
		}, cache),
		climateG: climateCache.Get,
		settingsG: util.Cached(func() (SettingsResponse, error) {
			return api.Settings(vin)
		}, cache),
		action: func(action, value string) error {
			return api.Action(vin, action, value)
		},
		wakeup: func() error {
			return api.WakeUp(vin)
		},
		connector:     connector,
		vehicleOnline: true,
		resetStatusG:  statusCache.Reset,
		resetClimateG: climateCache.Reset,
	}

	// Subscribe and start listening for streaming messages
	recvC := connector.Subscribe(vin)
	go impl.listenStreaming(log, vin, recvC)

	return impl
}

// listenStreaming processes incoming MQTT streaming messages.
func (v *Provider) listenStreaming(log *util.Logger, vin string, recvC <-chan StreamingMessage) {
	for msg := range recvC {
		// Determine topic suffix from the full topic: {userID}/{vin}/{suffix}
		parts := strings.SplitN(msg.Topic, "/", 3)
		if len(parts) < 3 {
			log.ERROR.Printf("streaming %s: unexpected topic: %s", vin, msg.Topic)
			continue
		}
		suffix := parts[2]

		switch suffix {
		case "service-event/charging":
			v.handleChargingEvent(log, vin, msg.Payload)

		case "service-event/air-conditioning":
			log.DEBUG.Printf("streaming %s: air-conditioning event", vin)
			if v.resetClimateG != nil {
				v.resetClimateG()
			}

		case "service-event/vehicle-status/odometer":
			log.DEBUG.Printf("streaming %s: odometer event", vin)
			if v.resetStatusG != nil {
				v.resetStatusG()
			}

		case "vehicle-event/vehicle-connection-status-update":
			v.handleConnectionEvent(log, vin, msg.Payload)

		default:
			log.DEBUG.Printf("streaming %s: unhandled topic suffix: %s", vin, suffix)
		}
	}
}

// handleChargingEvent processes a charging service-event message.
func (v *Provider) handleChargingEvent(log *util.Logger, vin string, payload []byte) {
	var evt ChargingEvent
	if err := json.Unmarshal(payload, &evt); err != nil {
		log.ERROR.Printf("streaming %s: parse charging event: %v", vin, err)
		return
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	v.streaming = &StreamingData{
		Soc:           evt.Data.Soc,
		Range:         evt.Data.ChargedRange,
		TimeToFinish:  evt.Data.TimeToFinish,
		ChargingState: evt.Data.ChargingState,
		Mode:          evt.Data.Mode,
	}

	log.DEBUG.Printf("streaming %s: charging soc=%d range=%d state=%s", vin, evt.Data.Soc, evt.Data.ChargedRange, evt.Data.ChargingState)
}

// handleConnectionEvent processes a vehicle connection status update.
func (v *Provider) handleConnectionEvent(log *util.Logger, vin string, payload []byte) {
	var evt VehicleConnectionEvent
	if err := json.Unmarshal(payload, &evt); err != nil {
		log.ERROR.Printf("streaming %s: parse connection event: %v", vin, err)
		return
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	status := strings.ToLower(evt.Data.Status)
	log.DEBUG.Printf("streaming %s: connection status=%s", vin, status)

	switch status {
	case "offline":
		v.streaming = nil
		v.vehicleOnline = false
	case "online", "awake":
		v.vehicleOnline = true
	}
}

var _ api.Battery = (*Provider)(nil)

// Soc implements the api.Vehicle interface
func (v *Provider) Soc() (float64, error) {
	v.mu.RLock()
	if v.connector != nil && v.connector.Connected() && v.vehicleOnline && v.streaming != nil {
		soc := v.streaming.Soc
		v.mu.RUnlock()
		return float64(soc), nil
	}
	v.mu.RUnlock()

	// REST fallback
	res, err := v.chargerG()
	if err == nil {
		return float64(res.Status.Battery.StateOfChargeInPercent), nil
	}

	return 0, err
}

var _ api.ChargeState = (*Provider)(nil)

// Status implements the api.ChargeState interface
func (v *Provider) Status() (api.ChargeStatus, error) {
	v.mu.RLock()
	if v.connector != nil && v.connector.Connected() && v.vehicleOnline && v.streaming != nil {
		state := v.streaming.ChargingState
		v.mu.RUnlock()

		switch strings.ToLower(state) {
		case "charging":
			return api.StatusC, nil
		default:
			// Streaming doesn't include charger plug state, so we still need
			// climateG() to distinguish StatusA (disconnected) from StatusB (connected).
			res, err := v.climateG()
			if err == nil && res.ChargerConnectionState == "CONNECTED" {
				return api.StatusB, nil
			}
			return api.StatusA, err
		}
	}
	v.mu.RUnlock()

	// REST fallback
	status := api.StatusA // disconnected

	res, err := v.climateG()
	if err == nil {
		if res.ChargerConnectionState == "CONNECTED" {
			status = api.StatusB
		}
	}

	resChrg, err := v.chargerG()
	if err == nil {
		if resChrg.Status.State == "CHARGING" {
			status = api.StatusC
		}
	}

	return status, err
}

var _ api.VehicleFinishTimer = (*Provider)(nil)

// FinishTime implements the api.VehicleFinishTimer interface
func (v *Provider) FinishTime() (time.Time, error) {
	v.mu.RLock()
	if v.connector != nil && v.connector.Connected() && v.vehicleOnline && v.streaming != nil {
		remaining := v.streaming.TimeToFinish
		v.mu.RUnlock()
		return time.Now().Add(time.Duration(remaining) * time.Minute), nil
	}
	v.mu.RUnlock()

	// REST fallback
	res, err := v.chargerG()
	if err == nil {
		crg := res.Status

		// estimate not available
		if crg.State == "Error" || crg.ChargeType == "Invalid" {
			return time.Time{}, api.ErrNotAvailable
		}

		remaining := time.Duration(crg.RemainingTimeToFullyChargedInMinutes) * time.Minute
		return time.Now().Add(remaining), err
	}

	return time.Time{}, err
}

var _ api.VehicleRange = (*Provider)(nil)

// Range implements the api.VehicleRange interface
func (v *Provider) Range() (int64, error) {
	v.mu.RLock()
	if v.connector != nil && v.connector.Connected() && v.vehicleOnline && v.streaming != nil {
		rng := v.streaming.Range
		v.mu.RUnlock()
		return rng, nil
	}
	v.mu.RUnlock()

	// REST fallback
	res, err := v.chargerG()
	return res.Status.Battery.RemainingCruisingRangeInMeters / 1e3, err
}

var _ api.VehicleOdometer = (*Provider)(nil)

// Odometer implements the api.VehicleOdometer interface
func (v *Provider) Odometer() (float64, error) {
	res, err := v.statusG()
	return res.MileageInKm, err
}

var _ api.VehicleClimater = (*Provider)(nil)

// Climater implements the api.VehicleClimater interface
func (v *Provider) Climater() (bool, error) {
	res, err := v.climateG()
	return slices.Contains([]string{"COOLING", "HEATING", "HEATING_AUXILIARY", "VENTILATION", "ON"}, res.State), err
}

var _ api.SocLimiter = (*Provider)(nil)

// GetLimitSoc implements the api.SocLimiter interface
func (v *Provider) GetLimitSoc() (int64, error) {
	res, err := v.chargerG()
	if err == nil {
		if res.Settings.TargetStateOfChargeInPercent == nil {
			return 0, api.ErrNotAvailable
		}
		return int64(*res.Settings.TargetStateOfChargeInPercent), nil
	}

	return 0, err
}

var _ api.ChargeController = (*Provider)(nil)

// ChargeEnable implements the api.ChargeController interface
func (v *Provider) ChargeEnable(enable bool) error {
	action := map[bool]string{true: ActionChargeStart, false: ActionChargeStop}
	return v.action(ActionCharge, action[enable])
}

var _ api.Resurrector = (*Provider)(nil)

// WakeUp implements the api.Resurrector interface
func (v *Provider) WakeUp() error {
	return v.wakeup()
}
