package webui

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"keyop/core"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

const (
	sessionCookieName = "keyop_session"
	sessionDuration   = 24 * time.Hour
	stateKeyPasskey   = "webui_passkey_state" //nolint:gosec // not a credential, just a state store key
)

// passkeyState is persisted to the state store.
type passkeyState struct {
	UserID      []byte                `json:"userId"`
	Credentials []webauthn.Credential `json:"credentials"`
}

// passkeyUser implements webauthn.User for the single owner account.
type passkeyUser struct {
	id          []byte
	credentials []webauthn.Credential
}

func (u *passkeyUser) WebAuthnID() []byte                         { return u.id }
func (u *passkeyUser) WebAuthnName() string                       { return "owner" }
func (u *passkeyUser) WebAuthnDisplayName() string                { return "Owner" }
func (u *passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

type sessionEntry struct {
	expiresAt time.Time
}

// authManager handles passkey registration, authentication, and session management.
type authManager struct {
	wa     *webauthn.WebAuthn
	user   *passkeyUser
	userMu sync.RWMutex

	sessions   map[string]sessionEntry
	sessionsMu sync.Mutex

	challenge   *webauthn.SessionData
	challengeMu sync.Mutex

	ss  core.StateStore
	log core.Logger
}

func newAuthManager(rpID, rpOrigin, rpDisplayName string, ss core.StateStore, log core.Logger) (*authManager, error) {
	if rpID == "" {
		return nil, fmt.Errorf("passkey_rp_id is required when auth_enabled is true")
	}
	if rpOrigin == "" {
		return nil, fmt.Errorf("passkey_rp_origin is required when auth_enabled is true")
	}

	wa, err := webauthn.New(&webauthn.Config{
		RPID:          rpID,
		RPDisplayName: rpDisplayName,
		RPOrigins:     []string{rpOrigin},
	})
	if err != nil {
		return nil, fmt.Errorf("webauthn setup: %w", err)
	}

	am := &authManager{
		wa:       wa,
		sessions: make(map[string]sessionEntry),
		ss:       ss,
		log:      log,
	}

	// Load persisted user identity and credentials.
	var state passkeyState
	if ss != nil {
		if err := ss.Load(stateKeyPasskey, &state); err == nil && len(state.UserID) > 0 {
			am.user = &passkeyUser{id: state.UserID, credentials: state.Credentials}
		}
	}

	if am.user == nil {
		id := make([]byte, 32)
		if _, err := rand.Read(id); err != nil {
			return nil, fmt.Errorf("generate user id: %w", err)
		}
		am.user = &passkeyUser{id: id}
		am.saveState()
	}

	return am, nil
}

func (am *authManager) hasCredentials() bool {
	am.userMu.RLock()
	defer am.userMu.RUnlock()
	return len(am.user.credentials) > 0
}

func (am *authManager) saveState() {
	if am.ss == nil {
		return
	}
	am.userMu.RLock()
	state := passkeyState{UserID: am.user.id, Credentials: am.user.credentials}
	am.userMu.RUnlock()
	if err := am.ss.Save(stateKeyPasskey, state); err != nil {
		am.log.Error("passkey: failed to save state", "error", err)
	}
}

// isAuthenticated returns true if the request carries a valid session cookie.
func (am *authManager) isAuthenticated(r *http.Request) bool {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return false
	}
	am.sessionsMu.Lock()
	defer am.sessionsMu.Unlock()
	entry, ok := am.sessions[cookie.Value]
	if !ok || time.Now().After(entry.expiresAt) {
		delete(am.sessions, cookie.Value)
		return false
	}
	return true
}

func (am *authManager) issueSession(w http.ResponseWriter) {
	token := make([]byte, 32)
	_, _ = rand.Read(token)
	tokenStr := hex.EncodeToString(token)
	am.sessionsMu.Lock()
	am.sessions[tokenStr] = sessionEntry{expiresAt: time.Now().Add(sessionDuration)}
	am.sessionsMu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    tokenStr,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionDuration.Seconds()),
	})
}

// middleware wraps a handler, enforcing auth on all routes except public ones.
func (am *authManager) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		// Always allow auth endpoints, login page, and static assets needed by the login page.
		if strings.HasPrefix(p, "/auth/") ||
			p == "/login" ||
			strings.HasPrefix(p, "/css/") ||
			strings.HasPrefix(p, "/js/") ||
			strings.HasPrefix(p, "/images/") {
			next.ServeHTTP(w, r)
			return
		}
		if am.isAuthenticated(r) {
			next.ServeHTTP(w, r)
			return
		}
		// API and SSE: return 401 so JS can handle it.
		if strings.HasPrefix(p, "/api/") || p == "/events" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		http.Redirect(w, r, "/login", http.StatusFound)
	})
}

// --- HTTP handlers ---

// handleStatus returns authentication and registration state.
func (am *authManager) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{
		"authenticated": am.isAuthenticated(r),
		"registered":    am.hasCredentials(),
	})
}

func (am *authManager) handleRegisterBegin(w http.ResponseWriter, _ *http.Request) {
	am.userMu.RLock()
	user := &passkeyUser{id: am.user.id, credentials: am.user.credentials}
	am.userMu.RUnlock()

	creation, session, err := am.wa.BeginRegistration(user)
	if err != nil {
		am.log.Error("passkey: begin registration failed", "error", err)
		http.Error(w, "registration begin failed", http.StatusInternalServerError)
		return
	}

	am.challengeMu.Lock()
	am.challenge = session
	am.challengeMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(creation)
}

func (am *authManager) handleRegisterFinish(w http.ResponseWriter, r *http.Request) {
	am.challengeMu.Lock()
	session := am.challenge
	am.challenge = nil
	am.challengeMu.Unlock()

	if session == nil {
		http.Error(w, "no pending registration", http.StatusBadRequest)
		return
	}

	am.userMu.RLock()
	user := &passkeyUser{id: am.user.id, credentials: am.user.credentials}
	am.userMu.RUnlock()

	cred, err := am.wa.FinishRegistration(user, *session, r)
	if err != nil {
		am.log.Error("passkey: finish registration failed", "error", err)
		http.Error(w, "registration verification failed", http.StatusBadRequest)
		return
	}

	am.userMu.Lock()
	am.user.credentials = append(am.user.credentials, *cred)
	am.userMu.Unlock()
	am.saveState()

	am.issueSession(w)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (am *authManager) handleLoginBegin(w http.ResponseWriter, _ *http.Request) {
	assertion, session, err := am.wa.BeginDiscoverableLogin()
	if err != nil {
		am.log.Error("passkey: begin login failed", "error", err)
		http.Error(w, "login begin failed", http.StatusInternalServerError)
		return
	}

	am.challengeMu.Lock()
	am.challenge = session
	am.challengeMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(assertion)
}

func (am *authManager) handleLoginFinish(w http.ResponseWriter, r *http.Request) {
	am.challengeMu.Lock()
	session := am.challenge
	am.challenge = nil
	am.challengeMu.Unlock()

	if session == nil {
		http.Error(w, "no pending login", http.StatusBadRequest)
		return
	}

	am.userMu.RLock()
	creds := make([]webauthn.Credential, len(am.user.credentials))
	copy(creds, am.user.credentials)
	userID := am.user.id
	am.userMu.RUnlock()

	handler := func(_, _ []byte) (webauthn.User, error) {
		return &passkeyUser{id: userID, credentials: creds}, nil
	}

	_, updatedCred, err := am.wa.FinishPasskeyLogin(handler, *session, r)
	if err != nil {
		am.log.Error("passkey: finish login failed", "error", err)
		http.Error(w, "authentication failed", http.StatusUnauthorized)
		return
	}

	// Update sign count to prevent replay attacks.
	am.userMu.Lock()
	for i, c := range am.user.credentials {
		if string(c.ID) == string(updatedCred.ID) {
			am.user.credentials[i].Authenticator.SignCount = updatedCred.Authenticator.SignCount
			break
		}
	}
	am.userMu.Unlock()
	am.saveState()

	am.issueSession(w)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (am *authManager) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		am.sessionsMu.Lock()
		delete(am.sessions, cookie.Value)
		am.sessionsMu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
	http.Redirect(w, r, "/login", http.StatusFound)
}

// handleLoginPage serves login.html from the embedded filesystem.
func handleLoginPage(w http.ResponseWriter, _ *http.Request) {
	f, err := resourcesFS().Open("login.html")
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer func() { _ = f.Close() }()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	_, _ = io.Copy(w, f)
}
