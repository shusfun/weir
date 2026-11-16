package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/shusfun/weir/internal/browsertest"
	"github.com/shusfun/weir/internal/buildinfo"
	"github.com/shusfun/weir/packages/contracts"
)

type browserSession struct {
	ID              string    `json:"id"`
	Token           string    `json:"token"`
	Endpoint        string    `json:"endpoint"`
	ExpiresAt       time.Time `json:"expires_at"`
	Purpose         string    `json:"purpose"`
	MaxBytes        int64     `json:"max_bytes"`
	CertificateHash string    `json:"certificate_hash,omitempty"`
}

type sessionIssuer struct {
	mu              sync.Mutex
	sessions        map[string]browserSession
	plan            contracts.BrowserTestPlan
	boot            string
	endpoint        string
	key             ed25519.PrivateKey
	sync            func([]string) error
	certificateHash string
}

func (s *sessionIssuer) issue(purpose string) (browserSession, error) {
	if purpose != "https" && purpose != "webrtc" && purpose != "webtransport" {
		return browserSession{}, errors.New("unknown purpose")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id, err := randomID()
	if err != nil {
		return browserSession{}, err
	}
	traceID, err := randomID()
	if err != nil {
		return browserSession{}, err
	}
	now := time.Now().UTC()
	claim := contracts.BrowserTestClaim{ID: id, GateID: s.plan.GateID, UserID: 1, Boot: s.boot, Purpose: purpose, Bytes: contracts.BrowserTestMaxBytes, Expires: now.Add(contracts.BrowserTestLifetime), TraceID: traceID}
	token, err := contracts.SignBrowserTest(claim, s.key)
	if err != nil {
		return browserSession{}, err
	}
	next := make(map[string]browserSession, 3)
	tokens := []string{token}
	for p, current := range s.sessions {
		if p != purpose && current.ExpiresAt.After(now) {
			next[p] = current
			tokens = append(tokens, current.Token)
		}
	}
	if err := s.sync(tokens); err != nil {
		return browserSession{}, err
	}
	result := browserSession{ID: id, Token: token, Endpoint: s.endpoint, ExpiresAt: claim.Expires, Purpose: purpose, MaxBytes: claim.Bytes}
	if purpose == "webtransport" {
		result.CertificateHash = s.certificateHash
	}
	next[purpose] = result
	s.sessions = next
	return result, nil
}

func main() {
	certPath := flag.String("cert", "", "TLS certificate signed by a locally trusted test CA")
	keyPath := flag.String("key", "", "TLS private key for the test certificate")
	originValue := flag.String("origin", "http://127.0.0.1:22340", "loopback console origin allowed to request sessions")
	lifetime := flag.Duration("lifetime", 10*time.Minute, "maximum fixture lifetime (up to one hour)")
	stdinClose := flag.Bool("stdin-close", false, "stop when the parent closes stdin")
	readyPath := flag.String("ready", "", "write public listener metadata after startup")
	flag.Parse()
	if *lifetime <= 0 || *lifetime > time.Hour {
		log.Fatal("-lifetime must be positive and at most one hour")
	}
	if *certPath == "" || *keyPath == "" {
		log.Fatal("-cert and -key are required")
	}
	origin, err := url.Parse(*originValue)
	if err != nil || origin.User != nil || (origin.Scheme != "http" && origin.Scheme != "https") || !isLoopbackHost(origin.Hostname()) || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
		log.Fatal("-origin must be an HTTP(S) loopback origin without a path")
	}
	certificate, err := tls.LoadX509KeyPair(*certPath, *keyPath)
	if err != nil {
		log.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil {
		log.Fatal(err)
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		log.Fatal(err)
	}
	port, err := freeTCPPort()
	if err != nil {
		log.Fatal(err)
	}
	rtcPort, err := freeUDPPort()
	if err != nil {
		log.Fatal(err)
	}
	for rtcPort == port {
		rtcPort, err = freeUDPPort()
		if err != nil {
			log.Fatal(err)
		}
	}
	plan := contracts.BrowserTestPlan{GateID: 2147483000, Port: port, RTCPort: rtcPort, CertID: 1, Host: "127.0.0.1", Origin: origin.Scheme + "://" + origin.Host, PublicKey: base64.RawURLEncoding.EncodeToString(public)}
	service, err := browsertest.StartLoopback(plan, certificate)
	if err != nil {
		log.Fatal(err)
	}
	defer service.Close()

	endpoint := "https://" + net.JoinHostPort(plan.Host, fmt.Sprint(plan.Port))
	issuer := &sessionIssuer{plan: plan, boot: service.Observation().Boot, endpoint: endpoint, key: private, sync: service.Sync, certificateHash: contracts.BrowserTestCertificateHash(leaf, time.Now())}

	control, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/session", func(w http.ResponseWriter, r *http.Request) {
		if !allowOrigin(w, r, plan.Origin) {
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		purpose := r.URL.Query().Get("purpose")
		if purpose != "https" && purpose != "webrtc" && purpose != "webtransport" {
			http.Error(w, "unknown purpose", http.StatusBadRequest)
			return
		}
		session, err := issuer.issue(purpose)
		if err != nil {
			http.Error(w, "session synchronization failed", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(session)
	})
	mux.HandleFunc("/usage", func(w http.ResponseWriter, r *http.Request) {
		if !allowOrigin(w, r, plan.Origin) {
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(service.Observation())
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 3 * time.Second}
	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(signalCtx, *lifetime)
	defer cancel()
	if *stdinClose {
		go func() { _, _ = io.Copy(io.Discard, os.Stdin); stop() }()
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	go func() {
		if err := server.Serve(control); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("control server stopped: %v", err)
			stop()
		}
	}()
	info := map[string]any{"control_url": "http://" + control.Addr().String(), "endpoint": endpoint, "rtc_port": rtcPort, "build_revision": buildinfo.Revision, "binding": "loopback only"}
	if *readyPath != "" {
		raw, err := json.Marshal(info)
		if err != nil {
			log.Fatal(err)
		}
		if err = os.WriteFile(*readyPath+".tmp", raw, 0600); err != nil {
			log.Fatal(err)
		}
		if err = os.Rename(*readyPath+".tmp", *readyPath); err != nil {
			log.Fatal(err)
		}
	}
	if err = json.NewEncoder(os.Stdout).Encode(info); err != nil {
		log.Fatal(err)
	}
	<-ctx.Done()
}

func allowOrigin(w http.ResponseWriter, r *http.Request, origin string) bool {
	if r.Header.Get("Origin") != origin {
		http.Error(w, "origin rejected", http.StatusForbidden)
		return false
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Vary", "Origin")
	return true
}

func isLoopbackHost(host string) bool {
	return host == "localhost" || net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
}

func freeTCPPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func freeUDPPort() (int, error) {
	listener, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.LocalAddr().(*net.UDPAddr).Port, nil
}

func randomID() (string, error) {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}
