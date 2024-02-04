package vc

import (
	"errors"
	"strings"
	"sync"

	"github.com/evcc-io/evcc/api"
	"github.com/teslamotors/vehicle-command/pkg/connector/inet"
)

var (
	mu         sync.Mutex
	identities = make(map[string]*Identity)
)

func getInstance(subject string) *Identity {
	v, _ := identities[subject]
	return v
}

func addInstance(subject string, identity *Identity) {
	identities[subject] = identity
}

// apiError converts HTTP 408 error to ErrTimeout
func apiError(err error) error {
	if err != nil && (errors.Is(err, inet.ErrVehicleNotAwake) ||
		strings.HasSuffix(err.Error(), "408 Request Timeout") || strings.HasSuffix(err.Error(), "408 (Request Timeout)")) {
		err = api.ErrAsleep
	}
	return err
}
