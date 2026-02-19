package weatherWs2902c

import (
	"fmt"
	"keyop/core"
	"keyop/util"
	"net/http"
	"reflect"
	"strconv"

	"github.com/google/uuid"
)

type WeatherData struct {
	Barometer      float64 `json:"baromAbsIn"`
	Baromrelin     float64 `json:"baromRelIn"`
	Dailyrainin    float64 `json:"dailyRainIn"`
	Dateutc        string  `json:"dateUtc"`
	Eventrainin    float64 `json:"eventRainIn"`
	Freq           string  `json:"freq"`
	Hourlyrainin   float64 `json:"hourlyRainIn"`
	Humidity       int     `json:"humidity"`
	Humidityin     int     `json:"humidityIn"`
	Maxdailygust   float64 `json:"maxDailyGust"`
	Model          string  `json:"model"`
	Monthlyrainin  float64 `json:"monthlyRainIn"`
	Rainratein     float64 `json:"rainRateIn"`
	Solarradiation float64 `json:"solarRadiation"`
	Stationtype    string  `json:"stationType"`
	OutTemp        float64 `json:"tempF"`
	InTemp         float64 `json:"tempInF"`
	Totalrainin    float64 `json:"totalRainIn"`
	Uv             int     `json:"uv"`
	Weeklyrainin   float64 `json:"weeklyRainIn"`
	Wh65batt       int     `json:"wh65Batt"`
	Winddir        int     `json:"winnDir"`
	Windgustmph    float64 `json:"windGustMph"`
	Windspeedmph   float64 `json:"windSpeedMph"`
}

var fieldMetricNames = map[string]string{
	"Barometer":      "barometer",
	"Baromrelin":     "barometerRel",
	"Dailyrainin":    "rain",
	"Eventrainin":    "eventRain",
	"Hourlyrainin":   "hourlyRain",
	"Humidity":       "outHumidity",
	"Humidityin":     "inHumidity",
	"Maxdailygust":   "maxDailyGust",
	"Monthlyrainin":  "monthlyRain",
	"Rainratein":     "rainRate",
	"Solarradiation": "solarRadiation",
	"OutTemp":        "outTemp",
	"InTemp":         "inTemp",
	"Totalrainin":    "totalRain",
	"Uv":             "uv",
	"Weeklyrainin":   "weeklyRain",
	"Wh65batt":       "wh65Batt",
	"Winddir":        "windDir",
	"Windgustmph":    "windGust",
	"Windspeedmph":   "windSpeed",
}

type Service struct {
	Deps             core.Dependencies
	Cfg              core.ServiceConfig
	Port             int
	FieldMetricNames map[string]string
	WeatherChan      core.ChannelInfo
	MetricsChan      core.ChannelInfo
	TempChan         core.ChannelInfo
}

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

	if weatherChan, ok := cfg.Pubs["weather"]; ok {
		svc.WeatherChan = weatherChan
	}
	if metricsChan, ok := cfg.Pubs["metrics"]; ok {
		svc.MetricsChan = metricsChan
	}
	if tempChan, ok := cfg.Pubs["temp"]; ok {
		svc.TempChan = tempChan
	}

	return svc
}

func (svc *Service) ValidateConfig() []error {
	var errs []error
	logger := svc.Deps.MustGetLogger()

	if svc.Port == 0 {
		errs = append(errs, fmt.Errorf("weatherWs2902c: port is required in config"))
	}

	pubErrs := util.ValidateConfig("pubs", svc.Cfg.Pubs, []string{"weather", "metrics", "temp"}, logger)
	if len(pubErrs) > 0 {
		errs = append(errs, pubErrs...)
	}

	if overrides, ok := svc.Cfg.Config["fieldMetricNames"].(map[string]interface{}); ok {
		weatherDataType := reflect.TypeOf(WeatherData{})
		for k := range overrides {
			if _, found := weatherDataType.FieldByName(k); !found {
				errs = append(errs, fmt.Errorf("weatherWs2902c: invalid field name in fieldMetricNames: %s", k))
			}
		}
	}

	return errs
}

func (svc *Service) Initialize() error {
	return nil
}

func (svc *Service) Check() error {

	logger := svc.Deps.MustGetLogger()

	mux := http.NewServeMux()
	mux.HandleFunc("/weather", svc.handleWeather)

	addr := fmt.Sprintf(":%d", svc.Port)
	logger.Info("weatherWs2902c: starting weather data service", "addr", addr)

	go func() {
		if err := http.ListenAndServe(addr, mux); err != nil {
			logger.Error("weatherWs2902c: http server failed", "error", err)
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

	logger.Error("weatherWs2902c: received weather data request", "method", r.Method, "remoteAddr", r.RemoteAddr)

	if err := r.ParseForm(); err != nil {
		logger.Error("weatherWs2902c: failed to parse form data", "error", err)
		http.Error(w, "Error parsing form data", http.StatusBadRequest)
		return
	}

	logger.Error("weatherWs2902c: form data", "data", r.Form)

	data := svc.ParseWeatherData(r)

	logger.Debug("weatherWs2902c: received weather data", "data", data)

	// generate correlation id for this check to tie together the events and metrics in the backend
	msgUuid := uuid.New().String()

	msg := core.Message{
		Uuid:        msgUuid,
		ChannelName: svc.WeatherChan.Name,
		ServiceName: svc.Cfg.Name,
		Data:        data,
	}

	if err := messenger.Send(msg); err != nil {
		logger.Error("weatherWs2902c: failed to send message", "error", err)
	}

	intTempMetricName, intTempMetricExists := svc.FieldMetricNames["InTemp"]
	if !intTempMetricExists || intTempMetricName == "" {
		logger.Warn("weatherWs2902c: inTemp metric name not configured, skipping inTemp metric publication")
	} else {
		tempInMsg := core.Message{
			Uuid:        msgUuid,
			ChannelName: svc.TempChan.Name,
			ServiceName: svc.Cfg.Name + "-intemp",
			MetricName:  intTempMetricName,
			Metric:      data.InTemp,
		}
		if err := messenger.Send(tempInMsg); err != nil {
			logger.Error("weatherWs2902c: failed to send inTemp message", "error", err)
		}
	}

	outTempMetricName, outTempMetricExists := svc.FieldMetricNames["OutTemp"]
	if !outTempMetricExists || outTempMetricName == "" {
		logger.Warn("weatherWs2902c: outTemp metric name not configured, skipping outTemp metric publication")
	} else {
		tempOutMsg := core.Message{
			Uuid:        msgUuid,
			ChannelName: svc.TempChan.Name,
			ServiceName: svc.Cfg.Name + "-outtemp",
			MetricName:  outTempMetricName,
			Metric:      data.OutTemp,
		}
		if err := messenger.Send(tempOutMsg); err != nil {
			logger.Error("weatherWs2902c: failed to send outTemp message", "error", err)
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
				Uuid:        msgUuid,
				ChannelName: svc.MetricsChan.Name,
				ServiceName: svc.Cfg.Name,
				MetricName:  metricName,
			}

			switch v := fieldValue.(type) {
			case float64:
				metricMsg.Metric = v
			case int:
				metricMsg.Metric = float64(v)
			default:
				logger.Warn("weatherWs2902c: non-numeric field not expected", "fieldname", fieldName, "type", fmt.Sprintf("%T", fieldValue))
				// Skip non-numeric fields for individual metric publication
				continue
			}

			if err := messenger.Send(metricMsg); err != nil {
				logger.Error("weatherWs2902c: failed to send metric message", "error", err, "metric", metricName)
			}
		}
	}

	w.WriteHeader(http.StatusOK)
}

func (svc *Service) ParseWeatherData(req *http.Request) *WeatherData {
	baromabsin, _ := strconv.ParseFloat(req.FormValue("baromabsin"), 64)
	baromrelin, _ := strconv.ParseFloat(req.FormValue("baromrelin"), 64)
	dailyrainin, _ := strconv.ParseFloat(req.FormValue("dailyrainin"), 64)
	eventrainin, _ := strconv.ParseFloat(req.FormValue("eventrainin"), 64)
	hourlyrainin, _ := strconv.ParseFloat(req.FormValue("hourlyrainin"), 64)
	humidity, _ := strconv.Atoi(req.FormValue("humidity"))
	humidityin, _ := strconv.Atoi(req.FormValue("humidityin"))
	maxdailygust, _ := strconv.ParseFloat(req.FormValue("maxdailygust"), 64)
	monthlyrainin, _ := strconv.ParseFloat(req.FormValue("monthlyrainin"), 64)
	rainratein, _ := strconv.ParseFloat(req.FormValue("rainratein"), 64)
	solarradiation, _ := strconv.ParseFloat(req.FormValue("solarradiation"), 64)
	tempf, _ := strconv.ParseFloat(req.FormValue("tempf"), 64)
	tempinf, _ := strconv.ParseFloat(req.FormValue("tempinf"), 64)
	totalrainin, _ := strconv.ParseFloat(req.FormValue("totalrainin"), 64)
	uv, _ := strconv.Atoi(req.FormValue("uv"))
	weeklyrainin, _ := strconv.ParseFloat(req.FormValue("weeklyrainin"), 64)
	wh65batt, _ := strconv.Atoi(req.FormValue("wh65batt"))
	winddir, _ := strconv.Atoi(req.FormValue("winddir"))
	windgustmph, _ := strconv.ParseFloat(req.FormValue("windgustmph"), 64)
	windspeedmph, _ := strconv.ParseFloat(req.FormValue("windspeedmph"), 64)

	weatherdata := &WeatherData{
		Barometer:      baromabsin,
		Baromrelin:     baromrelin,
		Dailyrainin:    dailyrainin,
		Dateutc:        req.FormValue("dateutc"),
		Eventrainin:    eventrainin,
		Freq:           req.FormValue("freq"),
		Hourlyrainin:   hourlyrainin,
		Humidity:       humidity,
		Humidityin:     humidityin,
		Maxdailygust:   maxdailygust,
		Model:          req.FormValue("model"),
		Monthlyrainin:  monthlyrainin,
		Rainratein:     rainratein,
		Solarradiation: solarradiation,
		Stationtype:    req.FormValue("stationtype"),
		OutTemp:        tempf,
		InTemp:         tempinf,
		Totalrainin:    totalrainin,
		Uv:             uv,
		Weeklyrainin:   weeklyrainin,
		Wh65batt:       wh65batt,
		Winddir:        winddir,
		Windgustmph:    windgustmph,
		Windspeedmph:   windspeedmph,
	}
	return weatherdata
}
