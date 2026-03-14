// Package temp provides the temp package.
package temp

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"keyop/core"
	"os"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// Service represents a Service used by the package.
type Service struct {
	Deps       core.Dependencies
	Cfg        core.ServiceConfig
	DevicePath string
	MaxTemp    *float64
	db         **sql.DB
}

// NewService creates a new service using the provided dependencies and configuration.
func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	svc := &Service{
		Deps: deps,
		Cfg:  cfg,
	}

	if devicePath, ok := cfg.Config["devicePath"].(string); ok {
		svc.DevicePath = devicePath
	}

	if maxTemp, ok := cfg.Config["maxTemp"].(float64); ok {
		svc.MaxTemp = &maxTemp
	}

	return svc
}

// ValidateConfig validates the service configuration and returns any validation errors.
func (svc Service) ValidateConfig() []error {
	var errs []error

	if _, ok := svc.Cfg.Config["devicePath"].(string); !ok {
		errs = append(errs, fmt.Errorf("temp: devicePath not set in config"))
	}

	if val, ok := svc.Cfg.Config["maxTemp"]; ok {
		if _, ok := val.(float64); !ok {
			errs = append(errs, fmt.Errorf("temp: maxTemp must be a float"))
		}
	}

	return errs
}

// Initialize performs one-time startup required by the service (resource loading or connectivity checks).
func (svc Service) Initialize() error {

	if svc.DevicePath == "" {
		return fmt.Errorf("temp: devicePath not set")
	}

	if _, err := os.Stat(svc.DevicePath); err != nil {
		return fmt.Errorf("temp: device path %s does not exist: %w", svc.DevicePath, err)
	}

	return nil
}

// Event contains parsed temperature readings and metadata returned by the temp service (TempC, TempF, Raw, Error).
type Event struct {
	TempC float32 `json:"TempC,omitempty"`
	TempF float32 `json:"TempF,omitempty"`
	Raw   string  `json:"Raw,omitempty"`
	Error string  `json:"Error,omitempty"`
}

// Check performs the service's periodic work: collect data, evaluate state, and publish messages/metrics.
func (svc Service) Check() error {
	_, err := svc.temp()
	return err
}

func (svc Service) sendError(msg string, level string) error {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()

	if level == "error" {
		logger.Error("temp", "error", msg)
	} else {
		logger.Debug("temp", "error", msg)
	}

	errorEvent := core.ErrorEvent{
		Summary: msg,
		Text:    msg,
		Level:   level,
	}
	if err := messenger.Send(core.Message{
		Correlation: uuid.New().String(),
		ChannelName: svc.Cfg.Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Event:       "temp_error",
		Data:        errorEvent,
	}); err != nil {
		logger.Error("temp", "error sending error event", err)
	}
	return errors.New(msg)
}

func (svc Service) temp() (Event, error) {
	logger := svc.Deps.MustGetLogger()
	logger.Debug("temp check called")

	messenger := svc.Deps.MustGetMessenger()

	temp := Event{}

	contentBytes, err := os.ReadFile(svc.DevicePath) //nolint:gosec // devicePath is operator-configured (not user input)
	if err != nil {
		errorMsg := fmt.Sprintf("could not read from %s: %s", svc.DevicePath, err.Error())
		return temp, svc.sendError(errorMsg, "error")
	}

	content := string(contentBytes)

	if len(content) == 0 {
		errorMsg := fmt.Sprintf("no content retrieved from temp device %s", svc.DevicePath)
		return temp, svc.sendError(errorMsg, "error")
	}

	idx := strings.Index(content, "t=")

	temp.Raw = content[idx+2 : len(content)-1]
	logger.Debug("Ds18b20", "RAW TEMP", temp.Raw)

	tempInt, err := strconv.Atoi(temp.Raw)
	if err != nil {
		errorMsg := fmt.Sprintf("unable to convert temp string to int: %s: %s", temp.Raw, err.Error())
		return temp, svc.sendError(errorMsg, "error")
	}

	temp.TempC = float32(tempInt) / 1000
	temp.TempF = temp.TempC*9/5 + 32.0

	if svc.MaxTemp != nil && float64(temp.TempF) > *svc.MaxTemp {
		errorMsg := fmt.Sprintf("temperature %.3f exceeds max %.3f", temp.TempF, *svc.MaxTemp)
		return temp, svc.sendError(errorMsg, "warning")
	}

	logger.Debug("temp", "data", temp)

	metricPrefix, _ := svc.Cfg.Config["metricPrefix"].(string)
	metricName := svc.Cfg.Name
	if metricPrefix != "" {
		metricName = metricPrefix + svc.Cfg.Name
	}

	// generate correlation id for this check to tie together the events and metrics in the backend
	correlationID := uuid.New().String()

	// Create typed TempEvent payload (only sent on successful reads)
	tempEvent := core.TempEvent{
		TempC:      temp.TempC,
		TempF:      temp.TempF,
		Raw:        temp.Raw,
		Hostname:   svc.Cfg.Name, // Using service name as hostname context
		SensorName: svc.DevicePath,
	}

	eventErr := messenger.Send(core.Message{
		Correlation: correlationID,
		ChannelName: svc.Cfg.Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Event:       "temp_reading",
		Text:        fmt.Sprintf("%s is %.3f°F", svc.Cfg.Name, temp.TempF),
		Summary:     fmt.Sprintf("%s is %.1f°", svc.Cfg.Name, temp.TempF),
		MetricName:  metricName,
		Metric:      float64(temp.TempF),
		Data:        tempEvent,
	})
	if eventErr != nil {
		return temp, eventErr
	}

	metricErr := messenger.Send(core.Message{
		Correlation: correlationID,
		ChannelName: svc.Cfg.Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Event:       "temp_metric",
		MetricName:  metricName,
		Metric:      float64(temp.TempF),
		Text:        fmt.Sprintf("%s metric: %.3f°F", svc.Cfg.Name, temp.TempF),
		Summary:     fmt.Sprintf("%s is %.1f°", svc.Cfg.Name, temp.TempF),
	})
	return temp, metricErr
}

// SQLiteSchema returns the DDL for the temps table.
func (svc *Service) SQLiteSchema() string {
	return `CREATE TABLE IF NOT EXISTS temps (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME,
		service_name TEXT,
		service_type TEXT,
		hostname TEXT,
		event TEXT,
		temp_c REAL,
		temp_f REAL,
		data TEXT
	);
	ALTER TABLE temps ADD COLUMN temp_c REAL;
	ALTER TABLE temps ADD COLUMN temp_f REAL;`
}

// SQLiteInsert prepares an INSERT for incoming messages with temperature data.
func (svc *Service) SQLiteInsert(msg core.Message) (string, []any) {
	var dataJSON string
	if msg.Data != nil {
		if b, err := json.Marshal(msg.Data); err == nil {
			dataJSON = string(b)
		} else {
			svc.Deps.MustGetLogger().Warn("temps: failed to marshal data for sqlite insert", "error", err)
		}
	}

	// Extract temperature values from TempEvent
	var tempC, tempF float32
	if tp, ok := core.AsType[*core.TempEvent](msg.Data); ok {
		if tp != nil {
			tempC = tp.TempC
			tempF = tp.TempF
		}
	} else if tv, ok := core.AsType[core.TempEvent](msg.Data); ok {
		tempC = tv.TempC
		tempF = tv.TempF
	}

	return `INSERT INTO temps (timestamp, service_name, service_type, hostname, event, temp_c, temp_f, data) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		[]any{msg.Timestamp, msg.ServiceName, msg.ServiceType, msg.Hostname, msg.Event, tempC, tempF, dataJSON}
}

// SetSQLiteDB allows the runtime to provide a pointer to the DB instance.
func (svc *Service) SetSQLiteDB(db **sql.DB) {
	svc.db = db
}

// PayloadTypes returns the payload type names that this provider handles.
func (svc *Service) PayloadTypes() []string {
	return []string{"core.temp.v1", "temp"}
}
