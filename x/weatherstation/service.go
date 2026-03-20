// Package weatherstation integrates with weather stations and publishes sensor readings as metrics and events.
//
//nolint:revive
package weatherstation

import (
	"database/sql"
	"fmt"
	"keyop/core"
	"net/http"
	"reflect"
	"strconv"
	"time"

	"github.com/google/uuid"
)

// WeatherData models posted weather sensor data (temperature, humidity, pressure, etc.) expected by the /weather endpoint.
type WeatherData struct {
	Barometer      float64 `json:"baromAbsIn"`
	BarometerRel   float64 `json:"baromRelIn"`
	DailyRain      float64 `json:"dailyRainIn"`
	DateUTC        string  `json:"dateUtc"`
	EventRain      float64 `json:"eventRainIn"`
	Frequency      string  `json:"freq"`
	HourlyRain     float64 `json:"hourlyRainIn"`
	OutHumidity    int     `json:"humidity"`
	InHumidity     int     `json:"humidityIn"`
	MaxDailyGust   float64 `json:"maxDailyGust"`
	Model          string  `json:"model"`
	MonthlyRain    float64 `json:"monthlyRainIn"`
	RainRate       float64 `json:"rainRateIn"`
	SolarRadiation float64 `json:"solarRadiation"`
	StationType    string  `json:"stationType"`
	OutTemp        float64 `json:"tempF"`
	InTemp         float64 `json:"tempInF"`
	TotalRain      float64 `json:"totalRainIn"`
	UV             int     `json:"uv"`
	WeeklyRain     float64 `json:"weeklyRainIn"`
	Wh65Batt       int     `json:"wh65Batt"`
	WindDir        int     `json:"windDir"`
	WindGust       float64 `json:"windGustMph"`
	WindSpeed      float64 `json:"windSpeedMph"`
}

var fieldMetricNames = map[string]string{
	"Barometer":      "barometer",
	"BarometerRel":   "barometerRel",
	"DailyRain":      "rain",
	"EventRain":      "eventRain",
	"HourlyRain":     "hourlyRain",
	"OutHumidity":    "outHumidity",
	"InHumidity":     "inHumidity",
	"MaxDailyGust":   "maxDailyGust",
	"MonthlyRain":    "monthlyRain",
	"RainRate":       "rainRate",
	"SolarRadiation": "solarRadiation",
	"OutTemp":        "outTemp",
	"InTemp":         "inTemp",
	"TotalRain":      "totalRain",
	"UV":             "uv",
	"WeeklyRain":     "weeklyRain",
	"Wh65Batt":       "wh65Batt",
	"WindDir":        "windDir",
	"WindGust":       "windGust",
	"WindSpeed":      "windSpeed",
}

//time=2026-03-13T17:57:05.411-07:00 level=ERROR msg="service config validation error" name=weatherbot type=weatherstation error="weatherstation: invalid field name in fieldMetricNames: Uv"

// Service reads sensor data from WS2902C devices, normalizes measurements, and emits metrics for consumers.
type Service struct {
	Deps             core.Dependencies
	Cfg              core.ServiceConfig
	Port             int
	FieldMetricNames map[string]string
	db               **sql.DB
}

// NewService creates a new service using the provided dependencies and configuration.
func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	svc := &Service{
		Deps:             deps,
		Cfg:              cfg,
		FieldMetricNames: make(map[string]string),
	}

	// Set defaults
	for k, v := range fieldMetricNames {
		svc.FieldMetricNames[k] = v
	}

	// Override from config
	if overrides, ok := cfg.Config["fieldMetricNames"].(map[string]interface{}); ok {
		for k, v := range overrides {
			if vs, ok := v.(string); ok {
				svc.FieldMetricNames[k] = vs
			}
		}
	}

	if port, ok := cfg.Config["port"].(int); ok {
		svc.Port = port
	} else if portFloat, ok := cfg.Config["port"].(float64); ok {
		svc.Port = int(portFloat)
	}

	return svc
}

// ValidateConfig validates the service configuration and returns any validation errors.
func (svc *Service) ValidateConfig() []error {
	var errs []error

	if svc.Port == 0 {
		errs = append(errs, fmt.Errorf("weatherstation: port is required in config"))
	}

	if overrides, ok := svc.Cfg.Config["fieldMetricNames"].(map[string]interface{}); ok {
		weatherDataType := reflect.TypeOf(WeatherData{})
		for k := range overrides {
			if _, found := weatherDataType.FieldByName(k); !found {
				errs = append(errs, fmt.Errorf("weatherstation: invalid field name in fieldMetricNames: %s", k))
			}
		}
	}

	return errs
}

// Initialize performs one-time startup required by the service (resource loading or connectivity checks).
func (svc *Service) Initialize() error {
	return nil
}

// Name returns the service name for payload registration.
func (svc *Service) Name() string { return "weatherstation" }

// RegisterPayloads registers the WeatherStationEvent payload type with the messenger registry.
func (svc *Service) RegisterPayloads(reg core.PayloadRegistry) error {
	if err := reg.Register("weatherstation", func() any { return &core.WeatherStationEvent{} }); err != nil {
		if !core.IsDuplicatePayloadRegistration(err) {
			return fmt.Errorf("weatherstation: failed to register alias: %w", err)
		}
	}
	if err := reg.Register("weatherstation.event.v1", func() any { return &core.WeatherStationEvent{} }); err != nil {
		if !core.IsDuplicatePayloadRegistration(err) {
			return fmt.Errorf("weatherstation: failed to register weatherstation.event.v1: %w", err)
		}
	}
	return nil
}

// Check performs the service's periodic work: collect data, evaluate state, and publish messages/metrics.
func (svc *Service) Check() error {

	logger := svc.Deps.MustGetLogger()

	mux := http.NewServeMux()
	mux.HandleFunc("/weather", svc.handleWeather)

	addr := fmt.Sprintf(":%d", svc.Port)
	logger.Info("weatherstation: starting weather data service", "addr", addr)

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("weatherstation: http server failed", "error", err)
		}
	}()

	return nil
}

func (svc *Service) handleWeather(w http.ResponseWriter, r *http.Request) {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	logger.Error("weatherstation: received weather data request", "method", r.Method, "remoteAddr", r.RemoteAddr)

	if err := r.ParseForm(); err != nil {
		logger.Error("weatherstation: failed to parse form data", "error", err)
		http.Error(w, "Error parsing form data", http.StatusBadRequest)
		return
	}

	logger.Error("weatherstation: form data", "data", r.Form)

	data := svc.ParseWeatherData(r)

	logger.Debug("weatherstation: received weather data", "data", data)

	// generate correlation id for this check to tie together the events and metrics in the backend
	correlationId := uuid.New().String()

	// Create WeatherStationEvent from WeatherData
	weatherEvent := core.WeatherStationEvent{
		Barometer:      data.Barometer,
		BarometerRel:   data.BarometerRel,
		DailyRain:      data.DailyRain,
		DateUTC:        data.DateUTC,
		EventRain:      data.EventRain,
		Frequency:      data.Frequency,
		HourlyRain:     data.HourlyRain,
		OutHumidity:    data.OutHumidity,
		InHumidity:     data.InHumidity,
		MaxDailyGust:   data.MaxDailyGust,
		Model:          data.Model,
		MonthlyRain:    data.MonthlyRain,
		RainRate:       data.RainRate,
		SolarRadiation: data.SolarRadiation,
		StationType:    data.StationType,
		OutTemp:        data.OutTemp,
		InTemp:         data.InTemp,
		TotalRain:      data.TotalRain,
		UV:             data.UV,
		WeeklyRain:     data.WeeklyRain,
		Wh65Batt:       data.Wh65Batt,
		WindDir:        data.WindDir,
		WindGust:       data.WindGust,
		WindSpeed:      data.WindSpeed,
	}

	msg := core.Message{
		Correlation: correlationId,
		ChannelName: svc.Cfg.Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Event:       "weatherstation",
		Data:        weatherEvent,
	}

	if err := messenger.Send(msg); err != nil {
		logger.Error("weatherstation: failed to send message", "error", err)
	}

	intTempMetricName, intTempMetricExists := svc.FieldMetricNames["InTemp"]
	if !intTempMetricExists || intTempMetricName == "" {
		logger.Warn("weatherstation: inTemp metric name not configured, skipping inTemp metric publication")
	} else {
		tempInEvent := core.TempEvent{
			TempF:      float32(data.InTemp),
			TempC:      float32((data.InTemp - 32) * 5 / 9),
			Hostname:   data.Model,
			SensorName: "indoor",
		}
		tempInMsg := core.Message{
			Correlation: correlationId,
			ChannelName: svc.Cfg.Name,
			ServiceName: svc.Cfg.Name + "-intemp",
			ServiceType: svc.Cfg.Type,
			Event:       "temp_metric",
			MetricName:  intTempMetricName,
			Metric:      data.InTemp,
			Summary:     fmt.Sprintf("%s is %.1f°", svc.Cfg.Name, data.InTemp),
			Text:        fmt.Sprintf("%s is %.1f°", svc.Cfg.Name, data.InTemp),
			Data:        tempInEvent,
		}
		if err := messenger.Send(tempInMsg); err != nil {
			logger.Error("weatherstation: failed to send inTemp message", "error", err)
		}
	}

	outTempMetricName, outTempMetricExists := svc.FieldMetricNames["OutTemp"]
	if !outTempMetricExists || outTempMetricName == "" {
		logger.Warn("weatherstation: outTemp metric name not configured, skipping outTemp metric publication")
	} else {
		tempOutEvent := core.TempEvent{
			TempF:      float32(data.OutTemp),
			TempC:      float32((data.OutTemp - 32) * 5 / 9),
			Hostname:   data.Model,
			SensorName: "outdoor",
		}
		tempOutMsg := core.Message{
			Correlation: correlationId,
			ChannelName: svc.Cfg.Name,
			ServiceName: svc.Cfg.Name + "-outtemp",
			ServiceType: svc.Cfg.Type,
			Event:       "temp_metric",
			MetricName:  outTempMetricName,
			Metric:      data.OutTemp,
			Summary:     fmt.Sprintf("%s is %.1f°", svc.Cfg.Name, data.OutTemp),
			Text:        fmt.Sprintf("%s is %.1f°", svc.Cfg.Name, data.OutTemp),
			Data:        tempOutEvent,
		}
		if err := messenger.Send(tempOutMsg); err != nil {
			logger.Error("weatherstation: failed to send outTemp message", "error", err)
		}
	}

	rainRateMetricName, rainRateMetricExists := svc.FieldMetricNames["RainRate"]
	if !rainRateMetricExists || rainRateMetricName == "" {
		logger.Warn("weatherstation: rainRate metric name not configured, skipping rainRate metric publication")
	} else {
		rainOutMsg := core.Message{
			Correlation: correlationId,
			ChannelName: svc.Cfg.Name,
			ServiceName: svc.Cfg.Name + "-rain",
			ServiceType: svc.Cfg.Type,
			Event:       "rain_metric",
			MetricName:  rainRateMetricName,
			Metric:      data.RainRate,
		}
		if data.RainRate > 0 {
			rainOutMsg.Summary = "raining"
			rainOutMsg.Text = "raining"
		} else {
			rainOutMsg.Summary = "not raining"
			rainOutMsg.Text = "not raining"
		}
		if err := messenger.Send(rainOutMsg); err != nil {
			logger.Error("weatherstation: failed to send rainRate message", "error", err)
		}
	}

	// iterate over the fields in the weatherdata struct using reflection
	fields := reflect.ValueOf(data).Elem()
	for i := 0; i < fields.NumField(); i++ {
		field := fields.Field(i)
		fieldName := fields.Type().Field(i).Name
		if metricName, exists := svc.FieldMetricNames[fieldName]; exists && metricName != "" {

			fieldValue := field.Interface()
			logger.Debug("Publishing weather metric", "field", fieldName, "metric", metricName, "value", fieldValue)

			metricMsg := core.Message{
				Correlation: correlationId,
				ChannelName: svc.Cfg.Name,
				ServiceName: svc.Cfg.Name,
				ServiceType: svc.Cfg.Type,
				Event:       fmt.Sprintf("weather_metric_%s", fieldName),
				MetricName:  metricName,
			}

			switch v := fieldValue.(type) {
			case float64:
				metricMsg.Metric = v
			case int:
				metricMsg.Metric = float64(v)
			default:
				logger.Warn("weatherstation: non-numeric field not expected", "fieldname", fieldName, "type", fmt.Sprintf("%T", fieldValue))
				// Skip non-numeric fields for individual metric publication
				continue
			}

			if err := messenger.Send(metricMsg); err != nil {
				logger.Error("weatherstation: failed to send metric message", "error", err, "metric", metricName)
			}
		}
	}

	w.WriteHeader(http.StatusOK)
}

// ParseWeatherData extracts weather form values from the HTTP request and returns a WeatherData object.
func (svc *Service) ParseWeatherData(req *http.Request) *WeatherData {
	barometer, _ := strconv.ParseFloat(req.FormValue("baromabsin"), 64)
	barometerRel, _ := strconv.ParseFloat(req.FormValue("baromrelin"), 64)
	dailyRain, _ := strconv.ParseFloat(req.FormValue("dailyrainin"), 64)
	eventRain, _ := strconv.ParseFloat(req.FormValue("eventrainin"), 64)
	hourlyRain, _ := strconv.ParseFloat(req.FormValue("hourlyrainin"), 64)
	outHumidity, _ := strconv.Atoi(req.FormValue("humidity"))
	inHumidity, _ := strconv.Atoi(req.FormValue("humidityin"))
	maxDailyGust, _ := strconv.ParseFloat(req.FormValue("maxdailygust"), 64)
	monthlyRain, _ := strconv.ParseFloat(req.FormValue("monthlyrainin"), 64)
	rainRate, _ := strconv.ParseFloat(req.FormValue("rainratein"), 64)
	solarRadiation, _ := strconv.ParseFloat(req.FormValue("solarradiation"), 64)
	outTemp, _ := strconv.ParseFloat(req.FormValue("tempf"), 64)
	inTemp, _ := strconv.ParseFloat(req.FormValue("tempinf"), 64)
	totalRain, _ := strconv.ParseFloat(req.FormValue("totalrainin"), 64)
	uv, _ := strconv.Atoi(req.FormValue("uv"))
	weeklyRain, _ := strconv.ParseFloat(req.FormValue("weeklyrainin"), 64)
	wh65Batt, _ := strconv.Atoi(req.FormValue("wh65batt"))
	windDir, _ := strconv.Atoi(req.FormValue("winddir"))
	windGust, _ := strconv.ParseFloat(req.FormValue("windgustmph"), 64)
	windSpeed, _ := strconv.ParseFloat(req.FormValue("windspeedmph"), 64)

	weatherdata := &WeatherData{
		Barometer:      barometer,
		BarometerRel:   barometerRel,
		DailyRain:      dailyRain,
		DateUTC:        req.FormValue("dateutc"),
		EventRain:      eventRain,
		Frequency:      req.FormValue("freq"),
		HourlyRain:     hourlyRain,
		OutHumidity:    outHumidity,
		InHumidity:     inHumidity,
		MaxDailyGust:   maxDailyGust,
		Model:          req.FormValue("model"),
		MonthlyRain:    monthlyRain,
		RainRate:       rainRate,
		SolarRadiation: solarRadiation,
		StationType:    req.FormValue("stationtype"),
		OutTemp:        outTemp,
		InTemp:         inTemp,
		TotalRain:      totalRain,
		UV:             uv,
		WeeklyRain:     weeklyRain,
		Wh65Batt:       wh65Batt,
		WindDir:        windDir,
		WindGust:       windGust,
		WindSpeed:      windSpeed,
	}
	return weatherdata
}
