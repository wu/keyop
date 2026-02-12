package owntracks

import (
	"encoding/json"
	"fmt"
	"io"
	"keyop/core"
	"keyop/util"
	"net/http"

	"github.com/google/uuid"
)

type Service struct {
	Deps core.Dependencies
	Cfg  core.ServiceConfig
	Port int

	currentRegions []string
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	svc := &Service{
		Deps: deps,
		Cfg:  cfg,
	}

	port, portExists := svc.Cfg.Config["port"].(int)
	if portExists {
		svc.Port = port
	}

	return svc
}

func (svc *Service) ValidateConfig() []error {
	logger := svc.Deps.MustGetLogger()
	var errs []error

	pubErrs := util.ValidateConfig("pubs", svc.Cfg.Pubs, []string{"owntracks", "gps", "metrics", "events"}, logger)
	if len(pubErrs) > 0 {
		errs = append(errs, pubErrs...)
	}

	// check port
	_, portExists := svc.Cfg.Config["port"].(int)
	if !portExists {
		err := fmt.Errorf("owntracks: port not set in config")
		logger.Error(err.Error())
		errs = append(errs, err)
	}

	return errs
}

func (svc *Service) Initialize() error {
	logger := svc.Deps.MustGetLogger()

	mux := http.NewServeMux()
	mux.HandleFunc("/", svc.ServeHTTP)

	addr := fmt.Sprintf(":%d", svc.Port)
	logger.Info("starting owntracks http server", "addr", addr)

	go func() {
		if err := http.ListenAndServe(addr, mux); err != nil {
			logger.Error("owntracks http server failed", "error", err)
		}
	}()

	return nil
}

func (svc *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Error("failed to read request body", "error", err)
		http.Error(w, "Error reading body", http.StatusInternalServerError)
		return
	}
	//goland:noinspection GoUnhandledErrorResult
	defer r.Body.Close()

	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		logger.Error("failed to unmarshal owntracks json", "error", err, "body", string(body))
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	logger.Debug("received owntracks message", "data", data)

	msg := core.Message{
		ChannelName: svc.Cfg.Pubs["owntracks"].Name,
		ServiceType: svc.Cfg.Type,
		ServiceName: svc.Cfg.Name,
		Data:        data,
	}

	// use provided uuid as correlation id or generate a new one
	if uuidVal, ok := data["uuid"].(string); ok && uuidVal != "" {
		msg.Uuid = uuidVal
	} else {
		msg.Uuid = uuid.New().String()
	}

	err = messenger.Send(msg)
	if err != nil {
		logger.Error("failed to send owntracks message", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// gps
	if msgType, ok := data["_type"].(string); ok && msgType == "location" {
		gpsData := make(map[string]interface{})
		for _, field := range []string{"lat", "lon", "alt"} {
			if val, ok := data[field]; ok {
				gpsData[field] = val
			}
		}

		gpsMsg := core.Message{
			Uuid:        msg.Uuid,
			ChannelName: svc.Cfg.Pubs["gps"].Name,
			ServiceType: svc.Cfg.Type,
			ServiceName: svc.Cfg.Name,
			Data:        gpsData,
		}
		if err := messenger.Send(gpsMsg); err != nil {
			logger.Error("failed to send gps message", "error", err)
		}
	}

	// metrics
	if batt, ok := data["batt"].(float64); ok {
		metricsData := make(map[string]interface{})
		if tid, ok := data["tid"].(string); ok {
			metricsData["tid"] = tid
		}
		if device, ok := data["device"].(string); ok {
			metricsData["device"] = device
		}

		metricsMsg := core.Message{
			Uuid:        msg.Uuid,
			ChannelName: svc.Cfg.Pubs["metrics"].Name,
			ServiceType: svc.Cfg.Type,
			ServiceName: svc.Cfg.Name,
			Metric:      batt,
			Data:        metricsData,
		}
		if err := messenger.Send(metricsMsg); err != nil {
			logger.Error("failed to send metrics message", "error", err)
		}
	}

	// events
	if inregions, ok := data["inregions"].([]interface{}); ok {
		var newRegions []string
		for _, r := range inregions {
			if s, ok := r.(string); ok {
				newRegions = append(newRegions, s)
			}
		}

		eventsChannel := svc.Cfg.Pubs["events"].Name

		// check for entered regions
		for _, nr := range newRegions {
			found := false
			for _, cr := range svc.currentRegions {
				if nr == cr {
					found = true
					break
				}
			}
			if !found {
				eventMsg := core.Message{
					Uuid:        msg.Uuid,
					ChannelName: eventsChannel,
					ServiceType: svc.Cfg.Type,
					ServiceName: svc.Cfg.Name,
					Data: map[string]interface{}{
						"event":  "enter",
						"region": nr,
					},
				}
				if err := messenger.Send(eventMsg); err != nil {
					logger.Error("failed to send enter event message", "error", err)
				}
			}
		}

		// check for exited regions
		for _, cr := range svc.currentRegions {
			found := false
			for _, nr := range newRegions {
				if cr == nr {
					found = true
					break
				}
			}
			if !found {
				eventMsg := core.Message{
					Uuid:        msg.Uuid,
					ChannelName: eventsChannel,
					ServiceType: svc.Cfg.Type,
					ServiceName: svc.Cfg.Name,
					Data: map[string]interface{}{
						"event":  "exit",
						"region": cr,
					},
				}
				if err := messenger.Send(eventMsg); err != nil {
					logger.Error("failed to send exit event message", "error", err)
				}
			}
		}

		svc.currentRegions = newRegions
	}

	w.WriteHeader(http.StatusOK)
}

func (svc *Service) Check() error {
	return nil
}
