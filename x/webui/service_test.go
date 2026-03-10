package webui

import (
	"encoding/json"
	"keyop/core"
	"keyop/core/testutil"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type mockTabProvider struct {
	tab    TabInfo
	assets http.FileSystem
}

func (m *mockTabProvider) WebUITab() TabInfo {
	return m.tab
}

func (m *mockTabProvider) WebUIAssets() http.FileSystem {
	return m.assets
}

func TestWebUI_GetTabs(t *testing.T) {
	deps := core.Dependencies{}
	deps.SetLogger(&core.FakeLogger{})

	cfg := core.ServiceConfig{
		Type: "webui",
		Name: "webui",
		Config: map[string]interface{}{
			"port": 8081,
		},
	}

	svc := NewService(deps, cfg).(*Service)

	provider := &mockTabProvider{
		tab: TabInfo{
			ID:    "test",
			Title: "Test Tab",
		},
	}
	svc.RegisterProvider("testType", provider)

	req := httptest.NewRequest("GET", "/api/tabs", nil)
	rr := httptest.NewRecorder()

	svc.handleGetTabs(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var tabs []TabInfo
	err := json.Unmarshal(rr.Body.Bytes(), &tabs)
	assert.NoError(t, err)
	assert.Len(t, tabs, 1)
	assert.Equal(t, "test", tabs[0].ID)
}

func TestWebUI_Events(t *testing.T) {
	messenger := testutil.NewFakeMessenger()
	deps := core.Dependencies{}
	deps.SetLogger(&core.FakeLogger{})
	deps.SetMessenger(messenger)

	ctx, cancel := t.Context(), func() {}
	deps.SetContext(ctx)
	deps.SetCancel(cancel)

	cfg := core.ServiceConfig{
		Type: "webui",
		Name: "webui",
	}

	svc := NewService(deps, cfg).(*Service)

	// Start SSE handler in goroutine
	req := httptest.NewRequest("GET", "/events", nil)
	rr := httptest.NewRecorder()

	// We need a flusher that we can control or at least one that doesn't block forever
	// httptest.ResponseRecorder doesn't implement Flusher by default in a way that helps with real-time testing
	// but we can check if it registers the client.

	go svc.handleEvents(rr, req)

	// Wait a bit for client to register
	time.Sleep(100 * time.Millisecond)

	svc.clientsMu.Lock()
	clientCount := len(svc.clients)
	svc.clientsMu.Unlock()
	assert.Equal(t, 1, clientCount)

	// Test broadcast
	msg := []byte(`{"event":"test"}`)
	svc.broadcast(msg)

	// In a real test we'd want to verify the body of rr, but rr.Body is only populated when the handler returns or flushes.
}

func TestWebUI_GetAsset(t *testing.T) {
	deps := core.Dependencies{}
	deps.SetLogger(&core.FakeLogger{})

	cfg := core.ServiceConfig{
		Type: "webui",
		Name: "webui",
	}

	svc := NewService(deps, cfg).(*Service)

	// Create a dummy file system
	// Using a simple map-based FS or similar would be nice, but http.Dir is easy.
	// For testing purposes, we can use a temporary directory.
	tmpDir := t.TempDir()
	assetContent := "console.log('hello');"
	err := core.OsProvider{}.MkdirAll(tmpDir, 0755)
	assert.NoError(t, err)
	f, err := core.OsProvider{}.OpenFile(tmpDir+"/test.js", 0x2|0x40|0x200, 0644) // O_RDWR|O_CREATE|O_TRUNC
	assert.NoError(t, err)
	_, err = f.Write([]byte(assetContent))
	assert.NoError(t, err)
	f.Close()

	provider := &mockTabProvider{
		tab:    TabInfo{ID: "test"},
		assets: http.Dir(tmpDir),
	}
	svc.RegisterProvider("testType", provider)

	req := httptest.NewRequest("GET", "/api/assets/testType/test.js", nil)
	req.SetPathValue("type", "testType")
	req.SetPathValue("path", "test.js")
	rr := httptest.NewRecorder()

	svc.handleGetAsset(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, assetContent, rr.Body.String())
	assert.Equal(t, "text/javascript; charset=utf-8", rr.Header().Get("Content-Type"))
}
