// Package webui provides a simple web interface (UI + SSE) for keyop services.
package webui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"keyop/core"
	"net"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

// TabProvider defines the interface for services that want to contribute a tab to the WebUI.
type TabProvider interface {
	WebUITab() TabInfo
}

// ActionProvider allows a service to handle custom actions from the WebUI.
type ActionProvider interface {
	HandleWebUIAction(action string, params map[string]any) (any, error)
}

// MarkdownRenderer is an optional interface for services to pre-render markdown.
// However, the requirement says "logic for rendering the markdown should live in the webui plugin".
// So we use goldmark inside webui service.

// TabInfo contains metadata and content for a UI tab.
type TabInfo struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Icon           string `json:"icon,omitempty"`
	Content        string `json:"content"`                 // HTML or a template
	JSPath         string `json:"jsPath,omitempty"`        // Path to a JS module for this tab
	RenderMarkdown bool   `json:"renderMarkdown,omitzero"` // If true, body from actions will be rendered from markdown
}

// AssetProvider defines the interface for services that provide static assets (like JS files).
type AssetProvider interface {
	WebUIAssets() http.FileSystem
}

// Service provides a web interface for the system.
type Service struct {
	Deps core.Dependencies
	Cfg  core.ServiceConfig

	providers   map[string]TabProvider
	providersMu sync.RWMutex

	panelProviders   map[string]PanelProvider
	panelProvidersMu sync.RWMutex

	assetProviders   map[string]AssetProvider
	assetProvidersMu sync.RWMutex

	server *http.Server
	port   int

	clients   map[chan []byte]bool
	clientsMu sync.Mutex
}

// NewService creates a new Service.
func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	port := 8080
	if p, ok := cfg.Config["port"].(float64); ok {
		port = int(p)
	} else if p, ok := cfg.Config["port"].(int); ok {
		port = p
	}

	return &Service{
		Deps:           deps,
		Cfg:            cfg,
		providers:      make(map[string]TabProvider),
		panelProviders: make(map[string]PanelProvider),
		assetProviders: make(map[string]AssetProvider),
		port:           port,
		clients:        make(map[chan []byte]bool),
	}
}

// RegisterProvider registers a TabProvider and check if it's an AssetProvider.
func (svc *Service) RegisterProvider(serviceType string, provider TabProvider) {
	svc.providersMu.Lock()
	svc.providers[serviceType] = provider
	svc.providersMu.Unlock()

	if ap, ok := provider.(AssetProvider); ok {
		svc.assetProvidersMu.Lock()
		svc.assetProviders[serviceType] = ap
		svc.assetProvidersMu.Unlock()
	}
}

// ValidateConfig validates the web UI service configuration and returns any validation errors.
func (svc *Service) ValidateConfig() []error {
	var errs []error
	if len(svc.Cfg.Subs) == 0 {
		errs = append(errs, fmt.Errorf("webui service requires at least one subscription in 'subs'"))
	}
	return errs
}

// Initialize starts the web UI server and subscriptions required for operation.
func (svc *Service) Initialize() error {
	logger := svc.Deps.MustGetLogger()
	ctx := svc.Deps.MustGetContext()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/tabs", svc.handleGetTabs)
	mux.HandleFunc("GET /api/panels", svc.handleGetPanels)
	mux.HandleFunc("POST /api/tabs/{id}/action/{action}", svc.handleTabAction)
	mux.HandleFunc("GET /events", svc.handleEvents)
	mux.HandleFunc("GET /api/assets/{type}/{path...}", svc.handleGetAsset)
	// Serve project images (e.g., /images/keyop.png)
	mux.Handle("/images/", http.StripPrefix("/images/", http.FileServer(http.Dir("images"))))
	mux.Handle("/", http.FileServer(http.Dir("x/webui/resources")))

	svc.server = &http.Server{
		Addr:              fmt.Sprintf(":%d", svc.port),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
	}

	go func() {
		logger.Info("WebUI server starting", "port", svc.port)
		if err := svc.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("WebUI server failed", "error", err)
		}
	}()

	// Subscribe to all channels listed in the 'subs' section
	for _, subInfo := range svc.Cfg.Subs {
		if err := svc.subscribeToChannel(ctx, subInfo); err != nil {
			return err
		}
	}

	return nil
}

func (svc *Service) subscribeToChannel(ctx context.Context, subInfo core.ChannelInfo) error {
	messenger := svc.Deps.MustGetMessenger()
	logger := svc.Deps.MustGetLogger()

	channelName := subInfo.Name
	if channelName == "" {
		return fmt.Errorf("webui: subscription entry missing 'Name'")
	}

	remote := subInfo.Remote
	if remote == "" {
		remote = channelName
	}

	err := messenger.Subscribe(ctx, svc.Cfg.Name, channelName, svc.Cfg.Type, svc.Cfg.Name, subInfo.MaxAge, func(msg core.Message) error {
		data, err := json.Marshal(msg)
		if err != nil {
			return err
		}
		svc.broadcast(data)
		return nil
	})

	if err != nil {
		logger.Error("WebUI failed to subscribe to channel", "channel", channelName, "error", err)
		return err
	}

	logger.Info("WebUI subscribed to channel", "channel", channelName, "remote", remote)
	return nil
}

func (svc *Service) broadcast(data []byte) {
	svc.clientsMu.Lock()
	defer svc.clientsMu.Unlock()
	for client := range svc.clients {
		select {
		case client <- data:
		default:
			// Client slow, skip or drop
		}
	}
}

func (svc *Service) handleGetTabs(w http.ResponseWriter, _ *http.Request) {
	svc.providersMu.RLock()
	defer svc.providersMu.RUnlock()

	tabs := make([]TabInfo, 0, len(svc.providers))
	for _, p := range svc.providers {
		tabs = append(tabs, p.WebUITab())
	}

	// Sort tabs alphabetically by title
	sort.Slice(tabs, func(i, j int) bool {
		return tabs[i].Title < tabs[j].Title
	})

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(tabs)
}

func (svc *Service) handleTabAction(w http.ResponseWriter, r *http.Request) {
	logger := svc.Deps.MustGetLogger()
	tabID := r.PathValue("id")
	action := r.PathValue("action")

	svc.providersMu.RLock()
	var provider TabProvider
	for _, p := range svc.providers {
		if p.WebUITab().ID == tabID {
			provider = p
			break
		}
	}
	svc.providersMu.RUnlock()

	if provider == nil {
		logger.Warn("Tab not found", "tabID", tabID)
		http.Error(w, "Tab not found", http.StatusNotFound)
		return
	}

	actionProvider, ok := provider.(ActionProvider)
	if !ok {
		logger.Warn("Tab does not support actions", "tabID", tabID)
		http.Error(w, "Tab does not support actions", http.StatusNotImplemented)
		return
	}

	var params map[string]any
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
			logger.Warn("Failed to decode JSON", "error", err)
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}
	}

	result, err := actionProvider.HandleWebUIAction(action, params)
	if err != nil {
		logger.Error("HandleWebUIAction error", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Render markdown if requested by the tab
	if provider.WebUITab().RenderMarkdown {
		md := goldmark.New(
			goldmark.WithExtensions(extension.Table),
		)
		if resMap, ok := result.(map[string]string); ok {
			if reportMd, exists := resMap["report"]; exists {
				var buf bytes.Buffer
				if err := md.Convert([]byte(reportMd), &buf); err == nil {
					resMap["report"] = buf.String()
					result = resMap
				}
			}
		} else if resMap, ok := result.(map[string]any); ok {
			if reportMd, exists := resMap["report"].(string); exists {
				var buf bytes.Buffer
				if err := md.Convert([]byte(reportMd), &buf); err == nil {
					resMap["report"] = buf.String()
					result = resMap
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (svc *Service) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	messageChan := make(chan []byte, 10)
	svc.clientsMu.Lock()
	svc.clients[messageChan] = true
	svc.clientsMu.Unlock()

	defer func() {
		svc.clientsMu.Lock()
		delete(svc.clients, messageChan)
		svc.clientsMu.Unlock()
		close(messageChan)
	}()

	ctx := r.Context()
	logger := svc.Deps.MustGetLogger()
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-messageChan:
			if _, err := fmt.Fprintf(w, "data: %s\n\n", msg); err != nil {
				logger.Warn("webui: failed to write SSE message", "error", err)
			}
			flusher.Flush()
		}
	}
}

func (svc *Service) handleGetAsset(w http.ResponseWriter, r *http.Request) {
	serviceType := r.PathValue("type")
	path := r.PathValue("path")

	svc.assetProvidersMu.RLock()
	ap, ok := svc.assetProviders[serviceType]
	svc.assetProvidersMu.RUnlock()

	if !ok {
		http.Error(w, "Asset provider not found", http.StatusNotFound)
		return
	}

	fs := ap.WebUIAssets()
	if fs == nil {
		http.Error(w, "No assets provided", http.StatusNotFound)
		return
	}

	// Serve the file from the provided FileSystem
	// http.FileServer might not be ideal for a single file, but http.ServeFile is better if we have a File.
	// However, we have a FileSystem.
	file, err := fs.Open(path)
	if err != nil {
		http.Error(w, "Asset not found", http.StatusNotFound)
		return
	}
	logger := svc.Deps.MustGetLogger()
	defer func() {
		if err := file.Close(); err != nil {
			logger.Warn("webui: failed to close asset file", "path", path, "error", err)
		}
	}()

	stat, err := file.Stat()
	if err != nil {
		http.Error(w, "Failed to get asset info", http.StatusInternalServerError)
		return
	}

	http.ServeContent(w, r, path, stat.ModTime(), file)
}

// Check performs a basic health check of the web UI service.
func (svc *Service) Check() error {
	return nil
}
