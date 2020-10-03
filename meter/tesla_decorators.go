package meter

// Code generated by github.com/andig/cmd/tools/decorate.go. DO NOT EDIT.

import (
	"github.com/andig/evcc/api"
)

func decorateTesla(base api.Meter, meterEnergy func() (float64, error), battery func() (float64, error)) api.Meter {
	switch {
	case battery == nil && meterEnergy == nil:
		return base

	case battery == nil && meterEnergy != nil:
		return &struct {
			api.Meter
			api.MeterEnergy
		}{
			Meter: base,
			MeterEnergy: &decorateTeslaMeterEnergyImpl{
				meterEnergy: meterEnergy,
			},
		}

	case battery != nil && meterEnergy == nil:
		return &struct {
			api.Meter
			api.Battery
		}{
			Meter: base,
			Battery: &decorateTeslaBatteryImpl{
				battery: battery,
			},
		}

	case battery != nil && meterEnergy != nil:
		return &struct {
			api.Meter
			api.Battery
			api.MeterEnergy
		}{
			Meter: base,
			Battery: &decorateTeslaBatteryImpl{
				battery: battery,
			},
			MeterEnergy: &decorateTeslaMeterEnergyImpl{
				meterEnergy: meterEnergy,
			},
		}
	}

	return nil
}

type decorateTeslaBatteryImpl struct {
	battery func() (float64, error)
}

func (impl *decorateTeslaBatteryImpl) SoC() (float64, error) {
	return impl.battery()
}

type decorateTeslaMeterEnergyImpl struct {
	meterEnergy func() (float64, error)
}

func (impl *decorateTeslaMeterEnergyImpl) TotalEnergy() (float64, error) {
	return impl.meterEnergy()
}
