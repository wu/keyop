package runtime

import (
	"context"
	"net"
	"net/http"
	nethttppprof "net/http/pprof"
	"os"
	"runtime/pprof"
	"strings"
	"time"

	"github.com/wu/keyop/core"
)

// pprofAddrEnv opts the process into a pprof listener. Unset, no listener is
// started. Accepts a bare port ("6060"), a port with colon (":6060"), or a full
// host:port; the first two bind to loopback.
const pprofAddrEnv = "KEYOP_PPROF_ADDR"

// serviceLabel is the pprof label key under which a service's name is recorded.
// CPU and goroutine profiles carry goroutine labels, so samples taken while a
// service's code is running are attributable to that service:
//
//	go tool pprof -tagfocus=service=cpuMonitor http://127.0.0.1:6060/debug/pprof/profile
//
// Heap profiles do not carry labels, so this attributes CPU only.
const serviceLabel = "service"

// withServiceLabel runs fn with the service name attached as a pprof goroutine
// label. Goroutines fn starts inherit the label for their whole lifetime, so
// labelling Initialize covers the background workers a service spawns there,
// and labelling a Check covers whatever that Check fans out.
func withServiceLabel(ctx context.Context, service string, fn func(context.Context)) {
	if ctx == nil {
		ctx = context.Background()
	}
	pprof.Do(ctx, pprof.Labels(serviceLabel, service), fn)
}

// startPprofServer starts a pprof HTTP listener when pprofAddrEnv is set, and
// shuts it down when the dependency context is cancelled.
//
// A bind failure is logged rather than returned: profiling is a diagnostic
// aid, and a busy debug port must not stop the daemon from running.
func startPprofServer(deps core.Dependencies) {
	logger := deps.MustGetLogger()

	raw := os.Getenv(pprofAddrEnv)
	if raw == "" {
		return
	}

	addr := normalizePprofAddr(raw)
	if !isLoopbackAddr(addr) {
		logger.Warn("pprof listener is not bound to loopback; profiling endpoints will be reachable from the network",
			"env", pprofAddrEnv, "addr", addr)
	}

	// Register on a private mux: importing net/http/pprof wires the handlers
	// into http.DefaultServeMux, which would expose them from any other server
	// in this process that happens to use the default mux.
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", nethttppprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", nethttppprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", nethttppprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", nethttppprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", nethttppprof.Trace)

	// Listen eagerly so a bad address is reported at startup rather than on the
	// first request.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		logger.Error("pprof listener failed to start", "env", pprofAddrEnv, "addr", addr, "error", err)
		return
	}

	srv := &http.Server{
		Handler: mux,
		// No write timeout: /debug/pprof/profile holds the connection open for
		// the full sample duration (30s by default) and would be truncated.
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			logger.Error("pprof listener stopped", "error", err)
		}
	}()

	go func() {
		<-deps.MustGetContext().Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("pprof listener shutdown error", "error", err)
		}
	}()

	logger.Info("pprof listener started", "addr", ln.Addr().String())
}

// normalizePprofAddr expands a bare port or ":port" to an explicit loopback
// address, leaving a full host:port untouched. Defaulting to loopback keeps an
// unqualified value from binding to every interface.
func normalizePprofAddr(raw string) string {
	addr := strings.TrimSpace(raw)
	if addr == "" {
		return addr
	}
	if !strings.Contains(addr, ":") {
		return net.JoinHostPort("127.0.0.1", addr)
	}
	if strings.HasPrefix(addr, ":") {
		return net.JoinHostPort("127.0.0.1", addr[1:])
	}
	return addr
}

// isLoopbackAddr reports whether addr binds to a loopback interface. An address
// whose host is not a literal IP (a hostname) is treated as non-loopback unless
// it is "localhost", since resolution is not attempted here.
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
