package memoryMonitor

import (
	"fmt"
	"keyop/core"
	"keyop/util"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

type Service struct {
	Deps             core.Dependencies
	Cfg              core.ServiceConfig
	MetricName       string
	TotalMemoryBytes int64
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	return &Service{
		Deps: deps,
		Cfg:  cfg,
	}
}

func (svc *Service) ValidateConfig() []error {
	logger := svc.Deps.MustGetLogger()
	var errs []error

	errs = append(errs, util.ValidateConfig("pubs", svc.Cfg.Pubs, []string{"metrics"}, logger)...)

	if val, ok := svc.Cfg.Config["metric_name"]; ok {
		if _, ok := val.(string); !ok {
			errs = append(errs, fmt.Errorf("metric_name must be a string"))
		}
	}

	return errs
}

func (svc *Service) Initialize() error {
	logger := svc.Deps.MustGetLogger()
	osProvider := svc.Deps.MustGetOsProvider()

	metricName, _ := svc.Cfg.Config["metric_name"].(string)
	if metricName == "" {
		metricName = fmt.Sprintf("%s.utilized_percent", svc.Cfg.Name)
	}
	svc.MetricName = metricName

	if runtime.GOOS == "darwin" {
		cmd := osProvider.Command("sysctl", "-n", "hw.memsize")
		output, err := cmd.Output()
		if err != nil {
			logger.Error("Failed to get total memory via sysctl", "error", err)
			return err
		}

		totalMem, err := strconv.ParseInt(strings.TrimSpace(string(output)), 10, 64)
		if err != nil {
			logger.Error("Failed to parse sysctl output", "error", err, "output", string(output))
			return err
		}
		svc.TotalMemoryBytes = totalMem
	}

	return nil
}

func (svc *Service) Check() error {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()

	var utilizedPercent float64
	var err error

	if runtime.GOOS == "linux" {
		utilizedPercent, err = svc.getLinuxMemUsage()
	} else if runtime.GOOS == "darwin" {
		utilizedPercent, err = svc.getDarwinMemUsage()
	} else {
		err = fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	if err != nil {
		logger.Error("Failed to get memory usage", "error", err)
		return err
	}

	logger.Info("Memory utilization", "utilized_percent", fmt.Sprintf("%.2f%%", utilizedPercent))

	err = messenger.Send(core.Message{
		ChannelName: svc.Cfg.Pubs["metrics"].Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		MetricName:  svc.MetricName,
		Metric:      utilizedPercent,
		Status:      "ok",
		Text:        fmt.Sprintf("Memory utilization: %.2f%%", utilizedPercent),
	})
	if err != nil {
		logger.Error("Failed to send memory metric", "error", err)
		return err
	}

	return nil
}

func (svc *Service) getLinuxMemUsage() (float64, error) {
	osProvider := svc.Deps.MustGetOsProvider()
	f, err := osProvider.OpenFile("/proc/meminfo", 0, 0)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	buf := make([]byte, 2048)
	n, err := f.Read(buf)
	if err != nil {
		return 0, err
	}

	var memTotal, memAvailable float64
	lines := strings.Split(string(buf[:n]), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if fields[0] == "MemTotal:" {
			memTotal, _ = strconv.ParseFloat(fields[1], 64)
		} else if fields[0] == "MemAvailable:" {
			memAvailable, _ = strconv.ParseFloat(fields[1], 64)
		}
	}

	if memTotal == 0 {
		return 0, fmt.Errorf("could not find MemTotal in /proc/meminfo")
	}

	usage := (memTotal - memAvailable) / memTotal * 100
	return usage, nil
}

func (svc *Service) getDarwinMemUsage() (float64, error) {
	osProvider := svc.Deps.MustGetOsProvider()
	cmd := osProvider.Command("vm_stat")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, err
	}

	freeBytes, err := parseVmStat(string(output))
	if err != nil {
		return 0, err
	}

	utilizedPercent := (1 - (float64(freeBytes) / float64(svc.TotalMemoryBytes))) * 100
	return utilizedPercent, nil
}

func parseVmStat(output string) (int64, error) {
	// Example first line: "Mach Virtual Memory Statistics: (page size of 16384 bytes)"
	pageSizeRe := regexp.MustCompile(`page size of (\d+) bytes`)
	pageSizeMatch := pageSizeRe.FindStringSubmatch(output)
	if len(pageSizeMatch) < 2 {
		return 0, fmt.Errorf("could not find page size in vm_stat output")
	}
	pageSize, err := strconv.ParseInt(pageSizeMatch[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse page size: %w", err)
	}

	freePagesRe := regexp.MustCompile(`Pages free:\s+(\d+)`)
	fileBackedRe := regexp.MustCompile(`File-backed pages:\s+(\d+)`)

	var freePages int64
	var fileBackedPages int64
	var foundFree, foundFileBacked bool

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if match := freePagesRe.FindStringSubmatch(line); len(match) > 1 {
			pages, err := strconv.ParseInt(match[1], 10, 64)
			if err != nil {
				return 0, fmt.Errorf("failed to parse pages free value: %w", err)
			}
			freePages = pages
			foundFree = true
		}
		if match := fileBackedRe.FindStringSubmatch(line); len(match) > 1 {
			pages, err := strconv.ParseInt(match[1], 10, 64)
			if err != nil {
				return 0, fmt.Errorf("failed to parse file-backed pages value: %w", err)
			}
			fileBackedPages = pages
			foundFileBacked = true
		}
	}

	if !foundFree {
		return 0, fmt.Errorf("could not find 'Pages free' in vm_stat output")
	}
	if !foundFileBacked {
		return 0, fmt.Errorf("could not find 'File-backed pages' in vm_stat output")
	}

	return (freePages + fileBackedPages) * pageSize, nil
}
