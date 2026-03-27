package webui

import (
	"bytes"
	"encoding/json"
	"errors"
	"keyop/core"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// minimalTabProvider implements only TabProvider (no ActionProvider, no AssetProvider).
type minimalTabProvider struct {
	tab TabInfo
}

func (m *minimalTabProvider) WebUITab() TabInfo { return m.tab }

// mockActionProvider implements TabProvider + ActionProvider.
type mockActionProvider struct {
	tab    TabInfo
	result any
	err    error
}

func (m *mockActionProvider) WebUITab() TabInfo { return m.tab }
func (m *mockActionProvider) HandleWebUIAction(_ string, _ map[string]any) (any, error) {
	return m.result, m.err
}

// mockPanelProvider implements PanelProvider only.
type mockPanelProvider struct {
	panels []PanelInfo
}

func (m *mockPanelProvider) WebUIPanels() []PanelInfo { return m.panels }

// newTestService is a helper that creates a Service suitable for unit tests.
func newTestService() *Service {
	deps := core.Dependencies{}
	deps.SetLogger(&core.FakeLogger{})
	cfg := core.ServiceConfig{
		Type:   "webui",
		Name:   "webui",
		Config: map[string]interface{}{"port": 0},
	}
	return NewService(deps, cfg).(*Service)
}

// ---------------------------------------------------------------------------
// Priority 1: Simple methods
// ---------------------------------------------------------------------------

func TestCheck_ReturnsNil(t *testing.T) {
	svc := newTestService()
	assert.NoError(t, svc.Check())
}

func TestValidateConfig_NoSubs(t *testing.T) {
	svc := newTestService()
	svc.Cfg.Subs = nil
	errs := svc.ValidateConfig()
	assert.NotEmpty(t, errs, "expected at least one error when Subs is empty")
}

func TestValidateConfig_WithSubs(t *testing.T) {
	svc := newTestService()
	svc.Cfg.Subs = map[string]core.ChannelInfo{
		"events": {Name: "events"},
	}
	errs := svc.ValidateConfig()
	assert.Empty(t, errs, "expected no errors when Subs has at least one entry")
}

func TestWebUITab_ReturnsTabInfo(t *testing.T) {
	svc := newTestService()
	tab := svc.WebUITab()
	assert.NotEmpty(t, tab.ID, "tab ID must not be empty")
	assert.NotEmpty(t, tab.Title, "tab Title must not be empty")
}

func TestWebUIAssets_ReturnsFileSystem(t *testing.T) {
	svc := newTestService()
	fs := svc.WebUIAssets()
	assert.NotNil(t, fs)
}

// ---------------------------------------------------------------------------
// Priority 2: Panel provider
// ---------------------------------------------------------------------------

func TestRegisterPanelProvider_NoPanic(t *testing.T) {
	svc := newTestService()
	provider := &mockPanelProvider{panels: []PanelInfo{{ID: "p1", Title: "Panel 1"}}}
	// Should not panic
	svc.RegisterPanelProvider("test-type", provider)

	svc.panelProvidersMu.RLock()
	_, ok := svc.panelProviders["test-type"]
	svc.panelProvidersMu.RUnlock()
	assert.True(t, ok, "panel provider should be registered")
}

func TestHandleGetPanels_Empty(t *testing.T) {
	svc := newTestService()

	req := httptest.NewRequest("GET", "/api/panels", nil)
	rr := httptest.NewRecorder()
	svc.handleGetPanels(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var panels []PanelInfo
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &panels))
	assert.NotNil(t, panels) // may be empty slice, but must be valid JSON array
}

func TestHandleGetPanels_WithProvider(t *testing.T) {
	svc := newTestService()
	provider := &mockPanelProvider{panels: []PanelInfo{
		{ID: "p1", Title: "Panel One"},
		{ID: "p2", Title: "Panel Two"},
	}}
	svc.RegisterPanelProvider("test-type", provider)

	req := httptest.NewRequest("GET", "/api/panels", nil)
	rr := httptest.NewRecorder()
	svc.handleGetPanels(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var panels []PanelInfo
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &panels))
	assert.Len(t, panels, 2)

	ids := map[string]bool{}
	for _, p := range panels {
		ids[p.ID] = true
	}
	assert.True(t, ids["p1"])
	assert.True(t, ids["p2"])
}

// ---------------------------------------------------------------------------
// Priority 3: Dashboard panel order
// ---------------------------------------------------------------------------

func TestHandleGetPanelOrder_Empty(t *testing.T) {
	svc := newTestService()

	req := httptest.NewRequest("GET", "/api/dashboard/panel-order", nil)
	rr := httptest.NewRecorder()
	svc.handleGetPanelOrder(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp map[string][]string
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	_, hasOrder := resp["order"]
	assert.True(t, hasOrder, "response must contain 'order' key")
}

func TestHandleSavePanelOrder_Valid(t *testing.T) {
	svc := newTestService()

	body := `{"order":["panel1","panel2"]}`
	req := httptest.NewRequest("POST", "/api/dashboard/panel-order", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	svc.handleSavePanelOrder(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp map[string]string
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Equal(t, "ok", resp["status"])
}

func TestHandleSavePanelOrder_GetAfterSave(t *testing.T) {
	svc := newTestService()

	// Save
	body := `{"order":["alpha","beta","gamma"]}`
	postReq := httptest.NewRequest("POST", "/api/dashboard/panel-order", strings.NewReader(body))
	postReq.Header.Set("Content-Type", "application/json")
	postRR := httptest.NewRecorder()
	svc.handleSavePanelOrder(postRR, postReq)
	require.Equal(t, http.StatusOK, postRR.Code)

	// Get
	getReq := httptest.NewRequest("GET", "/api/dashboard/panel-order", nil)
	getRR := httptest.NewRecorder()
	svc.handleGetPanelOrder(getRR, getReq)
	require.Equal(t, http.StatusOK, getRR.Code)

	var resp map[string][]string
	require.NoError(t, json.Unmarshal(getRR.Body.Bytes(), &resp))
	assert.Equal(t, []string{"alpha", "beta", "gamma"}, resp["order"])
}

func TestHandleSavePanelOrder_InvalidJSON(t *testing.T) {
	svc := newTestService()

	req := httptest.NewRequest("POST", "/api/dashboard/panel-order", bytes.NewBufferString("not-json"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	svc.handleSavePanelOrder(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

// ---------------------------------------------------------------------------
// Priority 4: Tab action handler
// ---------------------------------------------------------------------------

func TestHandleTabAction_UnknownTab(t *testing.T) {
	svc := newTestService()

	req := httptest.NewRequest("POST", "/api/tabs/no-such-tab/action/do-it", nil)
	req.SetPathValue("id", "no-such-tab")
	req.SetPathValue("action", "do-it")
	rr := httptest.NewRecorder()
	svc.handleTabAction(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestHandleTabAction_ProviderNotActionProvider(t *testing.T) {
	svc := newTestService()
	provider := &minimalTabProvider{tab: TabInfo{ID: "plain-tab", Title: "Plain"}}
	svc.RegisterProvider("plain-type", provider)

	req := httptest.NewRequest("POST", "/api/tabs/plain-tab/action/ping", nil)
	req.SetPathValue("id", "plain-tab")
	req.SetPathValue("action", "ping")
	rr := httptest.NewRecorder()
	svc.handleTabAction(rr, req)

	assert.Equal(t, http.StatusNotImplemented, rr.Code)
}

func TestHandleTabAction_Success(t *testing.T) {
	svc := newTestService()
	provider := &mockActionProvider{
		tab:    TabInfo{ID: "action-tab", Title: "Action"},
		result: map[string]any{"message": "pong"},
	}
	svc.RegisterProvider("action-type", provider)

	body := `{"key":"value"}`
	req := httptest.NewRequest("POST", "/api/tabs/action-tab/action/ping", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = int64(len(body))
	req.SetPathValue("id", "action-tab")
	req.SetPathValue("action", "ping")
	rr := httptest.NewRecorder()
	svc.handleTabAction(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Equal(t, "pong", resp["message"])
}

func TestHandleTabAction_ActionError(t *testing.T) {
	svc := newTestService()
	provider := &mockActionProvider{
		tab: TabInfo{ID: "err-tab", Title: "Error"},
		err: errors.New("action failed"),
	}
	svc.RegisterProvider("err-type", provider)

	req := httptest.NewRequest("POST", "/api/tabs/err-tab/action/boom", nil)
	req.SetPathValue("id", "err-tab")
	req.SetPathValue("action", "boom")
	rr := httptest.NewRecorder()
	svc.handleTabAction(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}

// ---------------------------------------------------------------------------
// Priority 5: Asset serving
// ---------------------------------------------------------------------------

func TestHandleJSAsset_NotFound(t *testing.T) {
	svc := newTestService()

	req := httptest.NewRequest("GET", "/js/nonexistent.js", nil)
	req.SetPathValue("path", "nonexistent.js")
	rr := httptest.NewRecorder()
	svc.handleJSAsset(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestHandleCSSAsset_NotFound(t *testing.T) {
	svc := newTestService()

	req := httptest.NewRequest("GET", "/css/nonexistent.css", nil)
	req.SetPathValue("path", "nonexistent.css")
	rr := httptest.NewRecorder()
	svc.handleCSSAsset(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}
