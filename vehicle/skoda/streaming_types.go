package skoda

// StreamingData holds the latest streaming state for a vehicle
type StreamingData struct {
	Soc           int
	Range         int64  // chargedRange in km
	TimeToFinish  int64  // remaining minutes
	ChargingState string // "charging", "notReadyForCharging", etc.
	Mode          string // "manual", etc.
}

// StreamingMessage is the MQTT message envelope for streaming events
type StreamingMessage struct {
	Topic   string // full MQTT topic, e.g. "{userID}/{vin}/service-event/charging"
	Payload []byte // raw JSON payload
}

// ChargingEvent represents a service-event/charging payload
type ChargingEvent struct {
	Data struct {
		Soc           int    `json:"soc"`
		ChargedRange  int64  `json:"chargedRange"`
		TimeToFinish  int64  `json:"timeToFinish"`
		ChargingState string `json:"chargingState"`
		Mode          string `json:"mode"`
	} `json:"data"`
}

// AirConditioningEvent represents a service-event/air-conditioning payload
type AirConditioningEvent struct {
	Data struct {
		State string `json:"state"`
	} `json:"data"`
}

// OdometerEvent represents a service-event/vehicle-status/odometer payload
type OdometerEvent struct {
	Data struct {
		MileageInKm float64 `json:"mileageInKm"`
	} `json:"data"`
}

// VehicleConnectionEvent represents a vehicle-event/vehicle-connection-status-update payload
type VehicleConnectionEvent struct {
	Data struct {
		Status string `json:"status"` // "online", "offline", "awake"
	} `json:"data"`
}
