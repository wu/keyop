package owntracks

import "keyop/core"

// LocationEvent is published for GPS location updates.
type LocationEvent struct {
	Device string  `json:"device,omitempty"`
	Lat    float64 `json:"lat"`
	Lon    float64 `json:"lon"`
	Alt    float64 `json:"alt,omitempty"`
	Acc    float64 `json:"acc,omitempty"`
	Batt   float64 `json:"batt,omitempty"`
}

// PayloadType returns the payload type identifier for LocationEvent.
func (e LocationEvent) PayloadType() string { return "service.owntracks.location.v1" }

// LocationEnterEvent is published when a device enters a region.
type LocationEnterEvent struct {
	Device string  `json:"device,omitempty"`
	Region string  `json:"region"`
	Lat    float64 `json:"lat,omitempty"`
	Lon    float64 `json:"lon,omitempty"`
}

// PayloadType returns the payload type identifier for LocationEnterEvent.
func (e LocationEnterEvent) PayloadType() string { return "service.owntracks.enter.v1" }

// LocationExitEvent is published when a device exits a region.
type LocationExitEvent struct {
	Device string  `json:"device,omitempty"`
	Region string  `json:"region"`
	Lat    float64 `json:"lat,omitempty"`
	Lon    float64 `json:"lon,omitempty"`
}

// PayloadType returns the payload type identifier for LocationExitEvent.
func (e LocationExitEvent) PayloadType() string { return "service.owntracks.exit.v1" }

// Name satisfies the core.PayloadProvider interface.
func (svc *Service) Name() string { return "owntracks" }

// RegisterPayloads registers typed payloads with the core registry.
func (svc *Service) RegisterPayloads(reg core.PayloadRegistry) error {
	types := map[string]func() any{
		"service.owntracks.location.v1": func() any { return &LocationEvent{} },
		"service.owntracks.enter.v1":    func() any { return &LocationEnterEvent{} },
		"service.owntracks.exit.v1":     func() any { return &LocationExitEvent{} },
	}
	for name, factory := range types {
		if err := reg.Register(name, factory); err != nil {
			if !core.IsDuplicatePayloadRegistration(err) {
				return err
			}
		}
	}
	return nil
}
