package vehicle

import (
	"context"
	"net/http"
	"time"

	"github.com/evcc-io/evcc/api"
	"github.com/evcc-io/evcc/util"
	"github.com/evcc-io/evcc/util/request"
	"github.com/evcc-io/evcc/vehicle/skoda"
	"github.com/evcc-io/evcc/vehicle/skoda/service"
)

// https://gitlab.com/prior99/skoda

// Skoda is an api.Vehicle implementation for Skoda cars
type Skoda struct {
	*embed
	*skoda.Provider // provides the api implementations
}

func init() {
	registry.Add("skoda", NewSkodaFromConfig)
}

// NewSkodaFromConfig creates a new vehicle
func NewSkodaFromConfig(other map[string]any) (api.Vehicle, error) {
	cc := struct {
		embed               `mapstructure:",squash"`
		User, Password, VIN string
		Cache               time.Duration
		Timeout             time.Duration
		Streaming           bool
	}{
		Cache:   interval,
		Timeout: request.Timeout,
	}

	if err := util.DecodeOther(other, &cc); err != nil {
		return nil, err
	}

	if cc.User == "" || cc.Password == "" {
		return nil, api.ErrMissingCredentials
	}

	v := &Skoda{
		embed: &cc.embed,
	}

	var err error
	log := util.NewLogger("skoda").Redact(cc.User, cc.Password, cc.VIN)

	// use Skoda api to resolve list of vehicles
	ts, err := service.TokenRefreshServiceTokenSource(log, skoda.TRSParams, skoda.AuthParams, cc.User, cc.Password)
	if err != nil {
		return nil, err
	}

	api := skoda.NewAPI(log, ts)
	api.Client.Timeout = cc.Timeout

	vehicle, err := ensureVehicleEx(
		cc.VIN, api.Vehicles,
		func(v skoda.Vehicle) (string, error) {
			return v.VIN, nil
		},
	)

	if err == nil {
		vehicle, err = api.VehicleDetails(vehicle.VIN)
	}

	if err == nil {
		v.fromVehicle(vehicle.Name, float64(vehicle.Specification.Battery.CapacityInKWh))
	}

	// reuse tokenService to build provider
	if err == nil {
		api := skoda.NewAPI(log, ts)
		api.Client.Timeout = cc.Timeout

		if cc.Streaming {
			// 1. Create FCM client and get token
			fcmClient := skoda.NewFCMClient(log, &http.Client{}, cc.User)
			fcmToken, fcmErr := fcmClient.GetOrRegisterToken(context.Background())
			if fcmErr != nil {
				return nil, fcmErr
			}

			// 2. Register with MySkoda (idempotent)
			if regErr := api.RegisterFCMToken(fcmToken); regErr != nil {
				log.WARN.Printf("fcm myskoda registration: %v", regErr)
			}

			// 3. Extract userID from access token
			token, tokenErr := ts.Token()
			if tokenErr != nil {
				return nil, tokenErr
			}

			userID, idErr := skoda.UserIDFromToken(token)
			if idErr != nil {
				return nil, idErr
			}

			// 4. Create MQTT connector and streaming provider
			connector := skoda.NewMqttConnector(log, userID, fcmToken, ts)
			v.Provider = skoda.NewStreamingProvider(log, api, vehicle.VIN, cc.Cache, connector)
		} else {
			v.Provider = skoda.NewProvider(api, vehicle.VIN, cc.Cache)
		}
	}

	return v, err
}
