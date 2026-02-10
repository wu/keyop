package cpuMonitor

import (
	"fmt"
	"keyop/core"
	"keyop/util"
	"runtime"
	"strconv"
	"strings"
)

type Service struct {
	Deps          core.Dependencies
	Cfg           core.ServiceConfig
	lastTotal     uint64
	lastIdle      uint64
	initialized   bool
	cpuMetricName string
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	return &Service{
		Deps: deps,
		Cfg:  cfg,
	}
}

func (svc *Service) ValidateConfig() []error {
	logger := svc.Deps.MustGetLogger()
	errs := util.ValidateConfig("pubs", svc.Cfg.Pubs, []string{"metrics"}, logger)

	cpuMetricName, _ := svc.Cfg.Config["cpu_metric_name"].(string)
	if cpuMetricName == "" {
		if val, ok := svc.Cfg.Config["metric_name"].(string); !ok || val == "" {
			// It's okay if they are both empty, Initialize will use default
			logger.Debug("no cpu_metric_name or metric_name provided, using default")
		}
	}

	return errs
}

func (svc *Service) Initialize() error {
	svc.cpuMetricName, _ = svc.Cfg.Config["cpu_metric_name"].(string)
	if svc.cpuMetricName == "" {
		if val, ok := svc.Cfg.Config["metric_name"].(string); ok {
			svc.cpuMetricName = val
		}
	}
	if svc.cpuMetricName == "" {
		svc.cpuMetricName = fmt.Sprintf("%s.cpu", svc.Cfg.Name)
	}

	// Initialize last values to get a delta on first Check
	if runtime.GOOS == "linux" {
		total, idle, err := svc.getLinuxCpuStats()
		if err == nil {
			svc.lastTotal = total
			svc.lastIdle = idle
			svc.initialized = true
		}
	}
	return nil
}

func (svc *Service) Check() error {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()

	var cpuUsage float64
	var cpuErr error

	switch runtime.GOOS {
	case "linux":
		cpuUsage, cpuErr = svc.getLinuxCpuUsage()
	case "darwin":
		cpuUsage, cpuErr = svc.getDarwinUsage()
	default:
		cpuErr = fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	if cpuErr != nil {
		logger.Error("failed to get cpu metrics", "error", cpuErr)
		return cpuErr
	}

	err := messenger.Send(core.Message{
		ChannelName: svc.Cfg.Pubs["metrics"].Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		MetricName:  svc.cpuMetricName,
		Metric:      cpuUsage,
		Text:        fmt.Sprintf("CPU Usage: %.2f%%", cpuUsage),
	})
	if err != nil {
		logger.Error("failed to send cpu metric", "error", err)
	}

	return nil
}

func (svc *Service) getLinuxCpuStats() (uint64, uint64, error) {
	osProvider := svc.Deps.MustGetOsProvider()
	f, err := osProvider.OpenFile("/proc/stat", 0, 0)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()

	buf := make([]byte, 1024)
	n, err := f.Read(buf)
	if err != nil {
		return 0, 0, err
	}

	lines := strings.Split(string(buf[:n]), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "cpu ") {
			fields := strings.Fields(line)
			if len(fields) < 5 {
				return 0, 0, fmt.Errorf("invalid /proc/stat format")
			}

			var total uint64
			for i := 1; i < len(fields); i++ {
				val, _ := strconv.ParseUint(fields[i], 10, 64)
				total += val
			}
			idle, _ := strconv.ParseUint(fields[4], 10, 64)
			return total, idle, nil
		}
	}

	return 0, 0, fmt.Errorf("cpu line not found in /proc/stat")
}

func (svc *Service) getLinuxCpuUsage() (float64, error) {
	total, idle, err := svc.getLinuxCpuStats()
	if err != nil {
		return 0, err
	}

	if !svc.initialized {
		svc.lastTotal = total
		svc.lastIdle = idle
		svc.initialized = true
		return 0, nil
	}

	diffTotal := total - svc.lastTotal
	diffIdle := idle - svc.lastIdle

	svc.lastTotal = total
	svc.lastIdle = idle

	if diffTotal == 0 {
		return 0, nil
	}

	usage := float64(diffTotal-diffIdle) / float64(diffTotal) * 100
	return usage, nil
}

func (svc *Service) getDarwinUsage() (float64, error) {
	osProvider := svc.Deps.MustGetOsProvider()
	cmd := osProvider.Command("top", "-l", "1", "-n", "0")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, err
	}

	var cpuUsage float64
	var foundCpu bool

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "CPU usage:") {
			fields := strings.Fields(line)
			for i, field := range fields {
				if field == "idle" && i > 0 {
					idleStr := strings.TrimSuffix(fields[i-1], "%")
					idle, err := strconv.ParseFloat(idleStr, 64)
					if err == nil {
						cpuUsage = 100 - idle
						foundCpu = true
					}
				}
			}
		}
	}

	if !foundCpu {
		return 0, fmt.Errorf("could not parse CPU usage from top output")
	}

	return cpuUsage, nil
}
