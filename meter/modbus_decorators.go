package meter

// Code generated by github.com/andig/cmd/tools/decorate.go. DO NOT EDIT.

import (
	"github.com/andig/evcc/api"
)

func decorateModbus(base api.Meter, meterEnergy func() (float64, error), battery func() (float64, error)) api.Meter {
	switch {
	case battery == nil && meterEnergy == nil:
		return base

	case battery == nil && meterEnergy != nil:
		return &struct {
			api.Meter
			api.MeterEnergy
		}{
			Meter: base,
			MeterEnergy: &decorateModbusMeterEnergyImpl{
				meterEnergy: meterEnergy,
			},
		}

	case battery != nil && meterEnergy == nil:
		return &struct {
			api.Meter
			api.Battery
		}{
			Meter: base,
			Battery: &decorateModbusBatteryImpl{
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
			Battery: &decorateModbusBatteryImpl{
				battery: battery,
			},
			MeterEnergy: &decorateModbusMeterEnergyImpl{
				meterEnergy: meterEnergy,
			},
		}
	}

	return nil
}

type decorateModbusBatteryImpl struct {
	battery func() (float64, error)
}

func (impl *decorateModbusBatteryImpl) SoC() (float64, error) {
	return impl.battery()
}

type decorateModbusMeterEnergyImpl struct {
	meterEnergy func() (float64, error)
}

func (impl *decorateModbusMeterEnergyImpl) TotalEnergy() (float64, error) {
	return impl.meterEnergy()
}
