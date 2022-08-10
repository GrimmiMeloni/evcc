package charger

// Code generated by github.com/evcc-io/evcc/cmd/tools/decorate.go. DO NOT EDIT.

import (
	"github.com/evcc-io/evcc/api"
)

func decorateCustom(base *Charger, identifier func() (string, error)) api.Charger {
	switch {
	case identifier == nil:
		return base

	case identifier != nil:
		return &struct {
			*Charger
			api.Identifier
		}{
			Charger: base,
			Identifier: &decorateCustomIdentifierImpl{
				identifier: identifier,
			},
		}
	}

	return nil
}

type decorateCustomIdentifierImpl struct {
	identifier func() (string, error)
}

func (impl *decorateCustomIdentifierImpl) Identify() (string, error) {
	return impl.identifier()
}
