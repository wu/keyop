package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/gorilla/websocket"
)

func mustReadFile(p string) []byte {
	b, err := os.ReadFile(p)
	if err != nil {
		log.Fatalf("failed to read %s: %v", p, err)
	}
	return b
}

func main() {
	host := flag.String("host", "localhost", "server host or IP")
	port := flag.Int("port", 2323, "server port")
	mode := flag.String("mode", "hello-first", "mode: hello-first or subscribe-first")
	channels := flag.String("channels", "status,errors,alerts,taskmgr-out,heartbeat", "comma-separated channel list")
	flag.Parse()

	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("failed to get home dir: %v", err)
	}
	certPath := filepath.Join(home, ".keyop", "certs", "keyop-client.crt")
	keyPath := filepath.Join(home, ".keyop", "certs", "keyop-client.key")
	caPath := filepath.Join(home, ".keyop", "certs", "ca.crt")

	certPEM := mustReadFile(certPath)
	keyPEM := mustReadFile(keyPath)
	caPEM := mustReadFile(caPath)

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		log.Fatalf("failed to load client cert/key: %v", err)
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caPEM) {
		log.Fatalf("failed to append CA cert")
	}

	tlsCfg := &tls.Config{
		Certificates:       []tls.Certificate{cert},
		RootCAs:            caPool,
		InsecureSkipVerify: true, // server hostname may not match; we're just testing frame order
	}

	u := url.URL{Scheme: "wss", Host: fmt.Sprintf("%s:%d", *host, *port), Path: "/ws"}
	log.Printf("connecting to %s", u.String())
	dialer := websocket.Dialer{TLSClientConfig: tlsCfg}
	conn, resp, err := dialer.Dial(u.String(), nil)
	if err != nil {
		if resp != nil {
			body, _ := io.ReadAll(resp.Body)
			log.Fatalf("dial failed: %v; resp status=%s; body=%s", err, resp.Status, string(body))
		}
		log.Fatalf("dial failed: %v", err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			log.Printf("conn close error: %v", err)
		}
	}()

	log.Printf("connected (HTTP status %v)", resp.Status)

	// build channel list
	chList := splitAndTrim(*channels)

	if *mode == "subscribe-first" {
		// send subscribe without hello
		sub := map[string]interface{}{
			"type":     "subscribe",
			"channels": chList,
		}
		log.Printf("sending (subscribe-first): %s", mustMarshal(sub))
		if err := conn.WriteJSON(sub); err != nil {
			log.Fatalf("WriteJSON failed: %v", err)
		}

		// wait for server response
		readLoop(conn)
		return
	}

	// hello-first
	hello := map[string]interface{}{
		"v":            1,
		"type":         "hello",
		"clientId":     "probe-client",
		"capabilities": map[string]bool{"batch": true},
	}
	log.Printf("sending hello: %s", mustMarshal(hello))
	if err := conn.WriteJSON(hello); err != nil {
		log.Fatalf("WriteJSON hello failed: %v", err)
	}

	// wait for welcome or error (with timeout)
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		log.Fatalf("failed to set read deadline: %v", err)
	}
	_, message, err := conn.ReadMessage()
	if err != nil {
		log.Fatalf("failed to read welcome: %v", err)
	}
	log.Printf("received: %s", string(message))

	// clear deadline
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		log.Printf("failed to clear read deadline: %v", err)
	}

	// send subscribe
	sub := map[string]interface{}{
		"v":        1,
		"type":     "subscribe",
		"channels": chList,
	}
	log.Printf("sending subscribe: %s", mustMarshal(sub))
	if err := conn.WriteJSON(sub); err != nil {
		log.Fatalf("WriteJSON subscribe failed: %v", err)
	}

	// read responses
	readLoop(conn)
}

func splitAndTrim(s string) []string {
	var out []string
	for _, p := range splitComma(s) {
		p = trim(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func splitComma(s string) []string { return []string(split(s, ',')) }

func split(s string, sep rune) []string {
	var parts []string
	cur := ""
	for _, r := range s {
		if r == sep {
			parts = append(parts, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	parts = append(parts, cur)
	return parts
}

func trim(s string) string { return string([]byte(s)) }

func mustMarshal(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func readLoop(conn *websocket.Conn) {
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			log.Printf("read error: %v", err)
			return
		}
		log.Printf("recv: %s", string(msg))
	}
}
