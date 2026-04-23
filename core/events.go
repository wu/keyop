package core

import (
	"reflect"
	"time"
)

// TypedPayload is a marker interface for typed payloads.
type TypedPayload interface {
	PayloadType() string
}

// PayloadTypeProvider is implemented by services or providers that expose
// the payload type(s) they handle. This is used for registering schema
// providers with the sqlite service based on message DataType.
//
// Services that implement SchemaProvider and also provide PayloadTypes()
// will be registered with sqlite instances keyed by payload type (preferred).
// Legacy services that do not implement this interface will continue to be
// registered by service type for backward compatibility.
type PayloadTypeProvider interface {
	PayloadTypes() []string
}

// DeviceStatusEvent represents a common event for device status updates.
type DeviceStatusEvent struct {
	DeviceID string `json:"deviceId"`
	Status   string `json:"status"`
	Battery  int    `json:"battery,omitempty"`
}

func (d DeviceStatusEvent) PayloadType() string { return "core.device.status.v1" }

// MetricEvent represents a common event for metric data.
type MetricEvent struct {
	Hostname string  `json:"hostname,omitempty"`
	Name     string  `json:"name"`
	Value    float64 `json:"value"`
	Unit     string  `json:"unit,omitempty"`
}

func (m MetricEvent) PayloadType() string { return "core.metric.v1" }

// AlertEvent represents a common alert payload used by multiple services.
//
// Fields are intentionally generic to support different alert types.  Services should
// populate Summary and Text with human-friendly messages--refer to the 'conventions'
// document for more details.  The "level" field is optional but can be used to indicate
// the severity of the alert (e.g., "info", "warning", "critical").
// Timestamp indicates when the alert event was created, and Hostname indicates
// the instance name from the messaging library.
type AlertEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Hostname  string    `json:"hostname,omitempty"`
	Summary   string    `json:"summary"`
	Text      string    `json:"text"`
	Level     string    `json:"level,omitempty"` // e.g., "info", "warning", "critical"
}

func (a AlertEvent) PayloadType() string { return "core.alert.v1" }

// ErrorEvent represents an error event that services can emit.
// It carries summary and text information about an error that occurred.
// Fields are intentionally generic to support different error types.  Services should
// populate Summary and Text with human-friendly messages.  The "level" field is optional
// but can be used to indicate the severity of the error (e.g., "debug", "info", "warning", "error", "critical").
// Timestamp indicates when the error event was created, and Hostname indicates
// the instance name from the messaging library.
type ErrorEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Hostname  string    `json:"hostname,omitempty"`
	Summary   string    `json:"summary"`
	Text      string    `json:"text"`
	Level     string    `json:"level,omitempty"` // e.g., "debug", "info", "warning", "error", "critical"
}

func (e ErrorEvent) PayloadType() string { return "core.error.v1" }

// StatusEvent represents a status update event that services can emit.
// It carries current status information for a named component or service.
// Name identifies the component, Status is the current state, Details provides
// additional information, and Level indicates the severity (e.g., "ok", "warning", "critical").
type StatusEvent struct {
	Name     string `json:"name"`               // Unique identifier for this status
	Hostname string `json:"hostname,omitempty"` // Hostname where status originated
	Status   string `json:"status"`             // Current status value ("ok", "warning", "critical")
	Details  string `json:"details"`            // Additional status information
}

func (s StatusEvent) PayloadType() string { return "core.status.v1" }

// TempEvent represents a temperature reading event that services can emit.
// It carries temperature readings in both Celsius and Fahrenheit, plus metadata about the sensor.
// TempEvent is only sent for successful readings; errors are reported as ErrorEvent.
type TempEvent struct {
	TempC      float32   `json:"tempC"`         // Temperature in Celsius
	TempF      float32   `json:"tempF"`         // Temperature in Fahrenheit
	Hostname   string    `json:"hostname"`      // Hostname where the temperature was measured
	SensorName string    `json:"sensorName"`    // Name/identifier of the temperature sensor
	MetricName string    `json:"metricName"`    // Name of the metric for publishing
	Timestamp  time.Time `json:"timestamp"`     // Time when the reading was taken
	Raw        string    `json:"raw,omitempty"` // Raw sensor reading (if available)
}

func (t TempEvent) PayloadType() string { return "core.temp.v1" }

// SwitchCommand represents a command to set a named switch device to a target state.
type SwitchCommand struct {
	DeviceName string `json:"deviceName"` // Name of the switch device
	State      string `json:"state"`      // "ON" or "OFF"
}

func (s SwitchCommand) PayloadType() string { return "core.switch.command.v1" }

// SwitchEvent represents the current state of a named switch device.
type SwitchEvent struct {
	DeviceName string `json:"deviceName"` // Name of the switch device
	State      string `json:"state"`      // "ON" or "OFF"
}

func (s SwitchEvent) PayloadType() string { return "core.switch.v1" }

// WeatherStationEvent represents weather data from a weather station.
type WeatherStationEvent struct {
	Barometer      float64 `json:"barometer"`      // Absolute atmospheric pressure
	BarometerRel   float64 `json:"barometerRel"`   // Relative atmospheric pressure
	DailyRain      float64 `json:"dailyRain"`      // Daily rainfall in inches
	DateUTC        string  `json:"dateUtc"`        // UTC timestamp from weather station
	EventRain      float64 `json:"eventRain"`      // Event rainfall in inches
	Frequency      string  `json:"frequency"`      // Station transmission frequency
	HourlyRain     float64 `json:"hourlyRain"`     // Hourly rainfall in inches
	OutHumidity    int     `json:"outHumidity"`    // Outdoor humidity percentage
	InHumidity     int     `json:"inHumidity"`     // Indoor humidity percentage
	MaxDailyGust   float64 `json:"maxDailyGust"`   // Maximum daily wind gust in mph
	Model          string  `json:"model"`          // Weather station model
	MonthlyRain    float64 `json:"monthlyRain"`    // Monthly rainfall in inches
	RainRate       float64 `json:"rainRate"`       // Current rain rate in inches/hour
	SolarRadiation float64 `json:"solarRadiation"` // Solar radiation in W/m²
	StationType    string  `json:"stationType"`    // Weather station type
	OutTemp        float64 `json:"outTemp"`        // Outdoor temperature in Fahrenheit
	InTemp         float64 `json:"inTemp"`         // Indoor temperature in Fahrenheit
	TotalRain      float64 `json:"totalRain"`      // Total rainfall in inches
	UV             int     `json:"uv"`             // UV index
	WeeklyRain     float64 `json:"weeklyRain"`     // Weekly rainfall in inches
	Wh65Batt       int     `json:"wh65Batt"`       // WH65 sensor battery level
	WindDir        int     `json:"windDir"`        // Wind direction in degrees
	WindGust       float64 `json:"windGust"`       // Wind gust in mph
	WindSpeed      float64 `json:"windSpeed"`      // Wind speed in mph
}

func (w WeatherStationEvent) PayloadType() string { return "weatherstation.event.v1" }

// GpsEvent represents GPS location data from a device.
type GpsEvent struct {
	Device    string    `json:"device"`
	Latitude  float64   `json:"latitude"`
	Longitude float64   `json:"longitude"`
	Altitude  float64   `json:"altitude,omitempty"`
	Accuracy  float64   `json:"accuracy,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

func (g GpsEvent) PayloadType() string { return "core.gps.v1" }

// ExtractAlertEvent attempts to retrieve a core.AlertEvent from the provided data.
// It supports direct AlertEvent typed values/pointers and structs that embed AlertEvent.
func ExtractAlertEvent(data any) (*AlertEvent, bool) {
	if data == nil {
		return nil, false
	}
	if aePtr, ok := AsType[*AlertEvent](data); ok && aePtr != nil {
		return aePtr, true
	}
	if aeVal, ok := AsType[AlertEvent](data); ok {
		return &aeVal, true
	}
	v := reflect.ValueOf(data)
	for v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil, false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil, false
	}
	t := v.Type()
	alertType := reflect.TypeOf(AlertEvent{})
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		f := v.Field(i)
		// direct field of type AlertEvent or *AlertEvent
		if field.Type == alertType {
			aeVal := f.Interface().(AlertEvent)
			return &aeVal, true
		}
		if field.Type.Kind() == reflect.Ptr && field.Type.Elem() == alertType {
			if f.IsNil() {
				return nil, false
			}
			aePtr := f.Interface().(*AlertEvent)
			return aePtr, true
		}
		// also check anonymous embedding where field is anonymous
		if field.Anonymous {
			if field.Type == alertType {
				aeVal := f.Interface().(AlertEvent)
				return &aeVal, true
			}
			if field.Type.Kind() == reflect.Ptr && field.Type.Elem() == alertType {
				if f.IsNil() {
					return nil, false
				}
				aePtr := f.Interface().(*AlertEvent)
				return aePtr, true
			}
		}
	}
	return nil, false
}
