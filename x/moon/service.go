package moon

import (
	"fmt"
	"keyop/core"
	"keyop/util"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/sj14/astral/pkg/astral"
)

type Service struct {
	Deps          core.Dependencies
	Cfg           core.ServiceConfig
	lastMoonPhase float64
	mu            sync.Mutex
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	return &Service{
		Deps:          deps,
		Cfg:           cfg,
		lastMoonPhase: -1, // Initialize to an impossible value
	}
}

func (svc *Service) ValidateConfig() []error {
	logger := svc.Deps.MustGetLogger()
	errs := util.ValidateConfig("pubs", svc.Cfg.Pubs, []string{"events", "alerts"}, logger)
	return errs
}

func (svc *Service) Initialize() error {
	state := svc.Deps.MustGetStateStore()
	err := state.Load(svc.Cfg.Name, &svc.lastMoonPhase)
	if err != nil {
		logger := svc.Deps.MustGetLogger()
		logger.Error("Failed to load moon phase state", "error", err)
	}
	return nil
}

func (svc *Service) Check() error {
	now := time.Now()
	phase := astral.MoonPhase(now)

	svc.mu.Lock()
	lastPhase := svc.lastMoonPhase
	svc.lastMoonPhase = phase
	svc.mu.Unlock()

	// Save state
	state := svc.Deps.MustGetStateStore()
	if err := state.Save(svc.Cfg.Name, phase); err != nil {
		logger := svc.Deps.MustGetLogger()
		logger.Error("Failed to save moon phase state", "error", err)
	}

	logger := svc.Deps.MustGetLogger()
	logger.Debug("Calculating moon phase", "time", now, "phase", phase)

	messenger := svc.Deps.MustGetMessenger()
	correlationId := uuid.New().String()

	phaseName := getMoonPhaseName(phase)
	lastPhaseName := ""
	if lastPhase >= 0 {
		lastPhaseName = getMoonPhaseName(lastPhase)
	}

	// Send event message with details
	eventMsg := core.Message{
		Correlation: correlationId,
		ChannelName: svc.Cfg.Pubs["events"].Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Text:        fmt.Sprintf("Current moon phase: %s (%.2f)", phaseName, phase),
		Summary:     fmt.Sprintf("Moon: %s", phaseName),
		Data: map[string]interface{}{
			"phase": phase,
			"name":  phaseName,
		},
	}
	if err := messenger.Send(eventMsg); err != nil {
		return err
	}

	// Send alert if the phase name has changed
	if phaseName != lastPhaseName {
		alertMsg := core.Message{
			Correlation: correlationId,
			ChannelName: svc.Cfg.Pubs["alerts"].Name,
			ServiceName: svc.Cfg.Name,
			ServiceType: svc.Cfg.Type,
			Text:        fmt.Sprintf("The moon is now in the %s phase", phaseName),
			Summary:     fmt.Sprintf("Moon phase: %s", phaseName),
		}
		return messenger.Send(alertMsg)
	}

	return nil
}

// getMoonPhaseName returns the name of the moon phase based on the phase value (0-28)
func getMoonPhaseName(phase float64) string {
	// astral.MoonPhase returns a value between 0 and 27.99...
	// 0: New Moon
	// 7: First Quarter
	// 14: Full Moon
	// 21: Last Quarter

	if phase < 1 {
		return "New Moon"
	} else if phase < 6 {
		return "Waxing Crescent"
	} else if phase < 8 {
		return "First Quarter"
	} else if phase < 13 {
		return "Waxing Gibbous"
	} else if phase < 15 {
		return "Full Moon"
	} else if phase < 20 {
		return "Waning Gibbous"
	} else if phase < 22 {
		return "Last Quarter"
	} else if phase < 27 {
		return "Waning Crescent"
	} else {
		return "New Moon"
	}
}
