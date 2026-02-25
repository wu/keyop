package macosBluetoothBattery

import (
	"encoding/json"
	"fmt"
	"keyop/core"
	"keyop/util"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

// DeviceBattery holds the name and battery percentage of a Bluetooth device.
type DeviceBattery struct {
	Name    string
	Percent float64
}

type Service struct {
	Deps         core.Dependencies
	Cfg          core.ServiceConfig
	metricPrefix string
	// deviceMetrics maps a device's name to an explicit metric name.
	// Entries are populated from the "device_metrics" config key.
	// Devices not listed here fall back to "{metricPrefix}.{sanitized_name}".
	deviceMetrics map[string]string
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

	if runtime.GOOS != "darwin" {
		logger.Warn("macosBluetoothBattery is only supported on macOS")
	}

	return errs
}

func (svc *Service) Initialize() error {
	prefix, _ := svc.Cfg.Config["metric_prefix"].(string)
	if prefix == "" {
		prefix = fmt.Sprintf("%s.battery", svc.Cfg.Name)
	}
	svc.metricPrefix = prefix

	svc.deviceMetrics = make(map[string]string)
	if raw, ok := svc.Cfg.Config["device_metrics"]; ok {
		if m, ok := raw.(map[string]interface{}); ok {
			for device, metric := range m {
				if metricStr, ok := metric.(string); ok {
					svc.deviceMetrics[normaliseDeviceName(device)] = metricStr
				}
			}
		}
	}

	return nil
}

func (svc *Service) Check() error {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()

	if runtime.GOOS != "darwin" {
		err := fmt.Errorf("macosBluetoothBattery: unsupported platform: %s", runtime.GOOS)
		logger.Error("unsupported platform", "error", err)
		return err
	}

	devices, err := svc.getBluetoothBatteries()
	if err != nil {
		logger.Error("failed to get bluetooth battery levels", "error", err)
		return err
	}

	if len(devices) == 0 {
		logger.Info("no bluetooth devices with battery information found")
		return nil
	}

	for _, device := range devices {
		metricName, ok := svc.deviceMetrics[device.Name]
		if !ok {
			metricName = fmt.Sprintf("%s.%s", svc.metricPrefix, sanitizeName(device.Name))
		}
		logger.Info("bluetooth battery", "device", device.Name, "percent", device.Percent)
		err := messenger.Send(core.Message{
			ChannelName: svc.Cfg.Pubs["metrics"].Name,
			ServiceName: svc.Cfg.Name,
			ServiceType: svc.Cfg.Type,
			MetricName:  metricName,
			Metric:      device.Percent,
			Text:        fmt.Sprintf("%s battery: %.0f%%", device.Name, device.Percent),
		})
		if err != nil {
			logger.Error("failed to send bluetooth battery metric", "device", device.Name, "error", err)
		}
	}

	return nil
}

// getBluetoothBatteries correlates battery levels from ioreg with device names
// from system_profiler. ioreg provides the MAC address and BatteryPercent;
// system_profiler provides the MAC address and human-readable device name.
func (svc *Service) getBluetoothBatteries() ([]DeviceBattery, error) {
	osProvider := svc.Deps.MustGetOsProvider()

	// 1. Get MAC → battery map from ioreg.
	ioregCmd := osProvider.Command("ioreg", "-r", "-c", "AppleDeviceManagementHIDEventService", "-k", "BatteryPercent", "-l")
	ioregOut, err := ioregCmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("ioreg command failed: %w", err)
	}
	macBattery := parseIoregBatteries(string(ioregOut))

	if len(macBattery) == 0 {
		return nil, nil
	}

	// 2. Get MAC → name map from system_profiler.
	spCmd := osProvider.Command("system_profiler", "SPBluetoothDataType", "-json")
	spOut, err := spCmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("system_profiler command failed: %w", err)
	}
	macName := parseSystemProfilerNames(spOut)

	// 3. Correlate: for each MAC with a battery, look up the device name.
	var devices []DeviceBattery
	for mac, pct := range macBattery {
		name, ok := macName[mac]
		if !ok {
			// No name found — use the MAC address itself as a fallback.
			name = mac
		}
		devices = append(devices, DeviceBattery{Name: name, Percent: pct})
	}

	return devices, nil
}

// parseIoregBatteries parses ioreg output and returns a map of
// normalised MAC address → battery percent.
func parseIoregBatteries(output string) map[string]float64 {
	result := make(map[string]float64)

	addrRe := regexp.MustCompile(`"DeviceAddress"\s*=\s*"([^"]+)"`)
	batteryRe := regexp.MustCompile(`"BatteryPercent"\s*=\s*(\d+)`)

	blocks := strings.Split(output, "+-o")
	for _, block := range blocks[1:] {
		batteryMatches := batteryRe.FindStringSubmatch(block)
		if batteryMatches == nil {
			continue
		}
		addrMatches := addrRe.FindStringSubmatch(block)
		if addrMatches == nil {
			continue
		}

		mac := normaliseMac(addrMatches[1])
		pct, err := strconv.ParseFloat(batteryMatches[1], 64)
		if err != nil {
			continue
		}
		result[mac] = pct
	}

	return result
}

// spBluetoothData mirrors the relevant parts of system_profiler -json output.
type spBluetoothData struct {
	SPBluetoothDataType []struct {
		DeviceConnected    []map[string]spDevice `json:"device_connected"`
		DeviceNotConnected []map[string]spDevice `json:"device_not_connected"`
	} `json:"SPBluetoothDataType"`
}

type spDevice struct {
	Address string `json:"device_address"`
}

// parseSystemProfilerNames parses system_profiler JSON and returns a map of
// normalised MAC address → human-readable device name.
func parseSystemProfilerNames(data []byte) map[string]string {
	result := make(map[string]string)

	var sp spBluetoothData
	if err := json.Unmarshal(data, &sp); err != nil {
		return result
	}

	for _, entry := range sp.SPBluetoothDataType {
		for _, deviceMap := range entry.DeviceConnected {
			for name, dev := range deviceMap {
				if dev.Address != "" {
					result[normaliseMac(dev.Address)] = normaliseDeviceName(name)
				}
			}
		}
		for _, deviceMap := range entry.DeviceNotConnected {
			for name, dev := range deviceMap {
				if dev.Address != "" {
					result[normaliseMac(dev.Address)] = normaliseDeviceName(name)
				}
			}
		}
	}

	return result
}

// normaliseMac lowercases a MAC address and replaces dashes or colons with
// colons, so that addresses from ioreg and system_profiler compare equal.
func normaliseMac(mac string) string {
	mac = strings.ToLower(mac)
	mac = strings.ReplaceAll(mac, "-", ":")
	return mac
}

// normaliseDeviceName replaces Unicode curly apostrophes/quotes with their
// ASCII equivalents so that names from system_profiler (which uses U+2019 ' )
// match names typed in YAML config (which use a plain ASCII apostrophe ' ).
func normaliseDeviceName(name string) string {
	name = strings.ReplaceAll(name, "\u2018", "'")  // ' LEFT SINGLE QUOTATION MARK
	name = strings.ReplaceAll(name, "\u2019", "'")  // ' RIGHT SINGLE QUOTATION MARK
	name = strings.ReplaceAll(name, "\u201C", "\"") // " LEFT DOUBLE QUOTATION MARK
	name = strings.ReplaceAll(name, "\u201D", "\"") // " RIGHT DOUBLE QUOTATION MARK
	return name
}

// sanitizeName converts a device name into a metric-safe string by replacing
// spaces and special characters with underscores and lowercasing everything.
func sanitizeName(name string) string {
	name = strings.ToLower(name)
	re := regexp.MustCompile(`[^a-z0-9]+`)
	name = re.ReplaceAllString(name, "_")
	name = strings.Trim(name, "_")
	return name
}
