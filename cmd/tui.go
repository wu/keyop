package cmd

import (
	"context"
	"fmt"
	"keyop/core"
	"keyop/x/webSocketClient"
	"log/slog"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/lucasb-eyer/go-colorful"
	"github.com/rivo/tview"
	"github.com/spf13/cobra"
)

var (
	hostnameColors      = make(map[string]string)
	hostnameColorsMu    sync.Mutex
	serviceNameColors   = make(map[string]string)
	serviceNameColorsMu sync.Mutex
)

type hostStatus struct {
	Hostname      string    `json:"hostname"`
	Uptime        string    `json:"uptime"`
	UptimeSeconds int64     `json:"uptimeSeconds"`
	LastSeen      time.Time `json:"lastSeen"`
}

type tempStatus struct {
	Hostname    string    `json:"hostname"`
	TempF       float32   `json:"tempF"`
	LastSeen    time.Time `json:"lastSeen"`
	ServiceName string    `json:"serviceName"`
}

type monitorState struct {
	Hosts             map[string]*hostStatus `json:"hosts"`
	Temps             map[string]*tempStatus `json:"temps"`
	HostnameColors    map[string]string      `json:"hostnameColors"`
	ServiceNameColors map[string]string      `json:"serviceNameColors"`
}

type logEntry struct {
	hostname    string
	serviceName string
	text        string
	timestamp   time.Time
}

func NewMonitorCmd(deps core.Dependencies) *cobra.Command {
	var wsPort int
	var wsHost string
	var hbChannel string
	var tempChannel string
	var errorChannel string
	var alertChannel string

	cmd := &cobra.Command{
		Use:   "tui",
		Short: "TUI monitor for heartbeats, temperatures, errors, and alerts",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMonitor(deps, wsHost, wsPort, hbChannel, tempChannel, errorChannel, alertChannel)
		},
	}

	cmd.Flags().IntVarP(&wsPort, "port", "p", 8080, "WebSocket server port")
	cmd.Flags().StringVarP(&wsHost, "host", "H", "localhost", "WebSocket server host")
	cmd.Flags().StringVarP(&hbChannel, "heartbeat-channel", "c", "heartbeat", "Heartbeat channel to subscribe to")
	cmd.Flags().StringVarP(&tempChannel, "temp-channel", "t", "temp", "Temperature channel to subscribe to")
	cmd.Flags().StringVarP(&errorChannel, "error-channel", "e", "errors", "Error channel to subscribe to")
	cmd.Flags().StringVarP(&alertChannel, "alert-channel", "a", "alerts", "Alert channel to subscribe to")

	return cmd
}

func runMonitor(deps core.Dependencies, wsHost string, wsPort int, hbChannel, tempChannel, errorChannel, alertChannel string) error {
	messenger := deps.MustGetMessenger()

	osProvider := deps.MustGetOsProvider()
	tempDir, err := os.MkdirTemp("", "keyop-tui-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer func() {
		_ = osProvider.RemoveAll(tempDir)
	}()

	dataDir := filepath.Join(tempDir, "data")
	if err := osProvider.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}
	messenger.SetDataDir(dataDir)

	// Persist state in ~/.keyop/monitor_state
	home, err := osProvider.UserHomeDir()
	if err != nil {
		home = "."
	}
	persistentDataDir := filepath.Join(home, ".keyop", "monitor_state")
	deps.SetStateStore(core.NewFileStateStore(persistentDataDir, osProvider))

	logDir := filepath.Join(tempDir, "logs")
	if err := osProvider.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	logFileName := "keyop-tui." + time.Now().Format("20060102") + ".log"
	logFilePath := filepath.Join(logDir, logFileName)
	f, err := osProvider.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	logger := slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug}))
	deps.SetLogger(logger)

	// Initialize WebSocket client to receive heartbeats and temperatures
	wsCfg := core.ServiceConfig{
		Name: "monitorWS",
		Type: "webSocketClient",
		Subs: map[string]core.ChannelInfo{
			"heartbeatSub": {Name: hbChannel},
			"tempSub":      {Name: tempChannel},
			"errorSub":     {Name: errorChannel},
			"alertSub":     {Name: alertChannel},
		},
		Config: map[string]interface{}{
			"hostname": wsHost,
			"port":     wsPort,
		},
	}

	wsSvc := webSocketClient.NewService(deps, wsCfg)
	if err := wsSvc.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize websocket client: %w", err)
	}

	// Start WebSocket client in background
	go func() {
		for {
			select {
			case <-deps.MustGetContext().Done():
				return
			default:
				_ = wsSvc.Check()
				time.Sleep(time.Second)
			}
		}
	}()

	// TUI Setup
	app := tview.NewApplication()

	hbTable := tview.NewTable().SetBorders(false)
	hbTable.SetBorder(true)

	tempTable := tview.NewTable().SetBorders(false)
	tempTable.SetBorder(true)

	errorTable := tview.NewTable().SetBorders(false)
	errorTable.SetTitle(" ERRORS ").SetTitleAlign(tview.AlignLeft).SetBorder(true)

	alertTable := tview.NewTable().SetBorders(false)
	alertTable.SetTitle(" ALERTS ").SetTitleAlign(tview.AlignLeft).SetBorder(true)

	grid := tview.NewGrid().
		SetRows(0, 0).
		SetColumns(0, 0).
		SetBorders(false).
		AddItem(hbTable, 0, 0, 1, 1, 0, 0, false).
		AddItem(tempTable, 0, 1, 1, 1, 0, 0, false).
		AddItem(alertTable, 1, 0, 1, 1, 0, 0, false).
		AddItem(errorTable, 1, 1, 1, 1, 0, 0, false)

	mainFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(grid, 0, 1, true)

	app.SetRoot(mainFlex, true)

	hosts := make(map[string]*hostStatus)
	temps := make(map[string]*tempStatus)

	// Load initial state
	stateStore := deps.MustGetStateStore()
	var state monitorState
	if err := stateStore.Load("monitor", &state); err == nil {
		if state.Hosts != nil {
			hosts = state.Hosts
		}
		if state.Temps != nil {
			temps = state.Temps
		}
		hostnameColorsMu.Lock()
		if state.HostnameColors != nil {
			hostnameColors = state.HostnameColors
		}
		hostnameColorsMu.Unlock()
		serviceNameColorsMu.Lock()
		if state.ServiceNameColors != nil {
			serviceNameColors = state.ServiceNameColors
		}
		serviceNameColorsMu.Unlock()
	}

	var errors []*logEntry
	var alerts []*logEntry
	var mu sync.Mutex

	ctx, cancel := context.WithCancel(deps.MustGetContext())
	defer cancel()

	// Periodic save
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				mu.Lock()
				hostnameColorsMu.Lock()
				serviceNameColorsMu.Lock()
				s := monitorState{
					Hosts:             hosts,
					Temps:             temps,
					HostnameColors:    hostnameColors,
					ServiceNameColors: serviceNameColors,
				}
				_ = stateStore.Save("monitor", s)
				serviceNameColorsMu.Unlock()
				hostnameColorsMu.Unlock()
				mu.Unlock()
			}
		}
	}()

	// Seek to end of queues to avoid processing old messages
	_ = messenger.SeekToEnd(hbChannel, "monitorTUI_HB")
	_ = messenger.SeekToEnd(tempChannel, "monitorTUI_Temp")
	_ = messenger.SeekToEnd(errorChannel, "monitorTUI_Error")
	_ = messenger.SeekToEnd(alertChannel, "monitorTUI_Alert")

	// Subscribe to heartbeats
	err = messenger.Subscribe(ctx, "monitorTUI_HB", hbChannel, "monitor", "monitor", 0, func(msg core.Message) error {
		if msg.ServiceType == "heartbeat" {
			data, ok := msg.Data.(map[string]interface{})
			if !ok {
				return nil
			}

			uptime, _ := data["Uptime"].(string)
			uptimeSecondsVal, _ := data["UptimeSeconds"].(float64)

			mu.Lock()
			if h, ok := hosts[msg.Hostname]; ok {
				h.Uptime = uptime
				h.UptimeSeconds = int64(uptimeSecondsVal)
				h.LastSeen = msg.Timestamp
			} else {
				hosts[msg.Hostname] = &hostStatus{
					Hostname:      msg.Hostname,
					Uptime:        uptime,
					UptimeSeconds: int64(uptimeSecondsVal),
					LastSeen:      msg.Timestamp,
				}
			}
			mu.Unlock()
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Subscribe to temperatures
	err = messenger.Subscribe(ctx, "monitorTUI_Temp", tempChannel, "monitor", "monitor", 0, func(msg core.Message) error {
		if msg.ServiceType == "temp" {
			data, ok := msg.Data.(map[string]interface{})
			if !ok {
				return nil
			}

			tempF, _ := data["TempF"].(float64)

			mu.Lock()
			if t, ok := temps[msg.ServiceName]; ok {
				t.TempF = float32(tempF)
				t.LastSeen = msg.Timestamp
			} else {
				temps[msg.ServiceName] = &tempStatus{
					Hostname:    msg.Hostname,
					TempF:       float32(tempF),
					LastSeen:    msg.Timestamp,
					ServiceName: msg.ServiceName,
				}
			}
			mu.Unlock()
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Subscribe to errors
	err = messenger.Subscribe(ctx, "monitorTUI_Error", errorChannel, "monitor", "monitor", 0, func(msg core.Message) error {
		mu.Lock()
		defer mu.Unlock()
		entry := &logEntry{
			hostname:    msg.Hostname,
			serviceName: msg.ServiceName,
			text:        msg.Text,
			timestamp:   msg.Timestamp,
		}
		errors = append([]*logEntry{entry}, errors...)
		if len(errors) > 10 {
			errors = errors[:10]
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Subscribe to alerts
	err = messenger.Subscribe(ctx, "monitorTUI_Alert", alertChannel, "monitor", "monitor", 0, func(msg core.Message) error {
		mu.Lock()
		defer mu.Unlock()
		entry := &logEntry{
			hostname:    msg.Hostname,
			serviceName: msg.ServiceName,
			text:        msg.Text,
			timestamp:   msg.Timestamp,
		}
		alerts = append([]*logEntry{entry}, alerts...)
		if len(alerts) > 10 {
			alerts = alerts[:10]
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Draw loop
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		updateUI(app, hbTable, tempTable, errorTable, alertTable, hosts, temps, errors, alerts, &mu, stateStore) // Initial draw
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				updateUI(app, hbTable, tempTable, errorTable, alertTable, hosts, temps, errors, alerts, &mu, stateStore)
			}
		}
	}()

	return app.Run()
}

func updateUI(app *tview.Application, hbTable, tempTable, errorTable, alertTable *tview.Table, hosts map[string]*hostStatus, temps map[string]*tempStatus, errors []*logEntry, alerts []*logEntry, mu *sync.Mutex, stateStore core.StateStore) {
	mu.Lock()
	defer mu.Unlock()

	headerStyle := tcell.StyleDefault.Foreground(tcell.ColorSlateGray).Bold(true)

	// 1. HEARTBEATS
	hbTable.Clear()
	hbTable.SetCell(0, 0, tview.NewTableCell("HOSTNAME").SetStyle(headerStyle).SetExpansion(1))
	hbTable.SetCell(0, 1, tview.NewTableCell("UPTIME").SetStyle(headerStyle).SetExpansion(1))
	hbTable.SetCell(0, 2, tview.NewTableCell("LAST SEEN").SetStyle(headerStyle).SetExpansion(1))

	var sortedHosts []*hostStatus
	for _, h := range hosts {
		sortedHosts = append(sortedHosts, h)
	}
	sort.Slice(sortedHosts, func(i, j int) bool {
		now := time.Now()
		tenMinutes := 10 * time.Minute
		iOld := now.Sub(sortedHosts[i].LastSeen) > tenMinutes
		jOld := now.Sub(sortedHosts[j].LastSeen) > tenMinutes

		if iOld && jOld {
			return sortedHosts[i].LastSeen.After(sortedHosts[j].LastSeen)
		}
		if iOld {
			return true
		}
		if jOld {
			return false
		}

		if sortedHosts[i].UptimeSeconds != sortedHosts[j].UptimeSeconds {
			return sortedHosts[i].UptimeSeconds < sortedHosts[j].UptimeSeconds
		}
		return sortedHosts[i].Hostname < sortedHosts[j].Hostname
	})

	for i, hb := range sortedHosts {
		since := time.Since(hb.LastSeen).Round(time.Second)
		sinceStr := fmt.Sprintf("%v", since)
		if since < time.Minute {
			sinceStr = "<1m"
		}

		uptimeStr := hb.Uptime
		if hb.UptimeSeconds >= 3600 {
			d := time.Duration(hb.UptimeSeconds) * time.Second
			h := d / time.Hour
			m := (d % time.Hour) / time.Minute
			uptimeStr = fmt.Sprintf("%dh%dm", h, m)
		}

		hostStyle := tcell.StyleDefault.Foreground(getHostnameColor(hb.Hostname, stateStore, hosts, temps))
		hbTable.SetCell(i+1, 0, tview.NewTableCell(hb.Hostname).SetStyle(hostStyle))
		hbTable.SetCell(i+1, 1, tview.NewTableCell(uptimeStr).SetStyle(hostStyle))
		hbTable.SetCell(i+1, 2, tview.NewTableCell(sinceStr).SetStyle(hostStyle))
	}
	hbTable.ScrollToBeginning()

	// 2. TEMPERATURES
	tempTable.Clear()
	tempTable.SetCell(0, 0, tview.NewTableCell("HOSTNAME").SetStyle(headerStyle).SetExpansion(1))
	tempTable.SetCell(0, 1, tview.NewTableCell("TEMP").SetStyle(headerStyle).SetExpansion(1))
	tempTable.SetCell(0, 2, tview.NewTableCell("LAST SEEN").SetStyle(headerStyle).SetExpansion(1))

	var sortedTemps []*tempStatus
	for _, t := range temps {
		sortedTemps = append(sortedTemps, t)
	}
	sort.Slice(sortedTemps, func(i, j int) bool {
		now := time.Now()
		tenMinutes := 10 * time.Minute
		iOld := now.Sub(sortedTemps[i].LastSeen) > tenMinutes
		jOld := now.Sub(sortedTemps[j].LastSeen) > tenMinutes

		if iOld && jOld {
			return sortedTemps[i].LastSeen.After(sortedTemps[j].LastSeen)
		}
		if iOld {
			return true
		}
		if jOld {
			return false
		}

		if sortedTemps[i].TempF != sortedTemps[j].TempF {
			return sortedTemps[i].TempF < sortedTemps[j].TempF
		}
		return sortedTemps[i].Hostname < sortedTemps[j].Hostname
	})

	for i, t := range sortedTemps {
		since := time.Since(t.LastSeen).Round(time.Second)
		sinceStr := fmt.Sprintf("%v", since)
		if since < time.Minute {
			sinceStr = "<1m"
		}
		tempStr := fmt.Sprintf("%.1f°F", t.TempF)
		tempStyle := tcell.StyleDefault.Foreground(getServiceColor(t.ServiceName, stateStore, hosts, temps))
		tempTable.SetCell(i+1, 0, tview.NewTableCell(t.ServiceName).SetStyle(tempStyle))
		tempTable.SetCell(i+1, 1, tview.NewTableCell(tempStr).SetStyle(tempStyle))
		tempTable.SetCell(i+1, 2, tview.NewTableCell(sinceStr).SetStyle(tempStyle))
	}
	tempTable.ScrollToBeginning()

	// 3. ERRORS
	errorTable.Clear()

	for i, e := range errors {
		timeStr := e.timestamp.Format("15:04:05")
		style := tcell.StyleDefault.Foreground(getAgeColor(e.timestamp))
		errorTable.SetCell(i, 0, tview.NewTableCell(timeStr).SetStyle(style))
		errorTable.SetCell(i, 1, tview.NewTableCell(e.hostname).SetStyle(style))
		errorTable.SetCell(i, 2, tview.NewTableCell(e.serviceName).SetStyle(style))
		errorTable.SetCell(i, 3, tview.NewTableCell(e.text).SetStyle(style))
	}

	// 4. ALERTS
	alertTable.Clear()

	for i, a := range alerts {
		timeStr := a.timestamp.Format("15:04:05")
		style := tcell.StyleDefault.Foreground(getAgeColor(a.timestamp))
		alertTable.SetCell(i, 0, tview.NewTableCell(timeStr).SetStyle(style))
		alertTable.SetCell(i, 1, tview.NewTableCell(a.hostname).SetStyle(style))
		alertTable.SetCell(i, 2, tview.NewTableCell(a.serviceName).SetStyle(style))
		alertTable.SetCell(i, 3, tview.NewTableCell(a.text).SetStyle(style))
	}

	app.Draw()
}

func getHostnameColor(hostname string, stateStore core.StateStore, hosts map[string]*hostStatus, temps map[string]*tempStatus) tcell.Color {
	hostnameColorsMu.Lock()
	defer hostnameColorsMu.Unlock()

	if hex, ok := hostnameColors[hostname]; ok {
		return tcell.GetColor(hex)
	}

	// Generate a random high-quality color using go-colorful.
	// We use HSL to ensure the color is bright and visible on a dark background.
	// Hue is random (0-360), Saturation is high (0.7-1.0), and Luminance is medium-high (0.6-0.8).
	h := rand.Float64() * 360.0
	s := 0.7 + rand.Float64()*0.3
	l := 0.5 + rand.Float64()*0.2
	c := colorful.Hsl(h, s, l)
	hex := c.Hex()
	hostnameColors[hostname] = hex

	// Save state immediately when a new color is generated
	serviceNameColorsMu.Lock()
	s_ := monitorState{
		Hosts:             hosts,
		Temps:             temps,
		HostnameColors:    hostnameColors,
		ServiceNameColors: serviceNameColors,
	}
	_ = stateStore.Save("monitor", s_)
	serviceNameColorsMu.Unlock()

	return tcell.GetColor(hex)
}

func getServiceColor(serviceName string, stateStore core.StateStore, hosts map[string]*hostStatus, temps map[string]*tempStatus) tcell.Color {
	serviceNameColorsMu.Lock()
	defer serviceNameColorsMu.Unlock()

	if hex, ok := serviceNameColors[serviceName]; ok {
		return tcell.GetColor(hex)
	}

	// Generate a random high-quality color using go-colorful.
	// We use HSL to ensure the color is bright and visible on a dark background.
	// Hue is random (0-360), Saturation is high (0.7-1.0), and Luminance is medium-high (0.6-0.8).
	h := rand.Float64() * 360.0
	s := 0.7 + rand.Float64()*0.3
	l := 0.5 + rand.Float64()*0.2
	c := colorful.Hsl(h, s, l)
	hex := c.Hex()
	serviceNameColors[serviceName] = hex

	// Save state immediately when a new color is generated
	hostnameColorsMu.Lock()
	s_ := monitorState{
		Hosts:             hosts,
		Temps:             temps,
		HostnameColors:    hostnameColors,
		ServiceNameColors: serviceNameColors,
	}
	_ = stateStore.Save("monitor", s_)
	hostnameColorsMu.Unlock()

	return tcell.GetColor(hex)
}

func getAgeColor(timestamp time.Time) tcell.Color {
	age := time.Since(timestamp)
	if age >= 12*time.Hour {
		return tcell.GetColor("#333333") // Dark grey
	}

	// Interpolate between white (#FFFFFF) and dark grey (#333333)
	// Ratio goes from 0.0 (new) to 1.0 (12 hours old)
	ratio := age.Seconds() / (12 * time.Hour).Seconds()

	// Start color (white)
	start := colorful.Color{R: 1, G: 1, B: 1}
	// End color (dark grey)
	end := colorful.Color{R: 0.2, G: 0.2, B: 0.2}

	// Linear interpolation
	c := colorful.Color{
		R: start.R + (end.R-start.R)*ratio,
		G: start.G + (end.G-start.G)*ratio,
		B: start.B + (end.B-start.B)*ratio,
	}

	return tcell.GetColor(c.Hex())
}
