// Package browsertest serves bounded test data without accepting destinations.
package browsertest

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/pion/ice/v4"
	"github.com/pion/webrtc/v4"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/quic-go/quic-go/logging"
	"github.com/quic-go/webtransport-go"
	"github.com/shusfun/weir/internal/buildinfo"
	"github.com/shusfun/weir/packages/contracts"
)

type session struct {
	claim    contracts.BrowserTestClaim
	used     int64
	reserved int64
	started  bool
	active   bool
	ctx      context.Context
	cancel   context.CancelFunc
}

type Server struct {
	plan     contracts.BrowserTestPlan
	boot     string
	mu       sync.Mutex
	sessions map[string]*session
	http     *http.Server
	wt       *webtransport.Server
	rtc      *webrtc.API
	udpMux   ice.UDPMux
	tcp      net.Listener
	quic     net.PacketConn
	closed   bool
	failure  string
	wg       sync.WaitGroup
	slots    chan struct{}
}

// Start binds all three listeners before publishing a usable service.
func Start(plan contracts.BrowserTestPlan, certificate tls.Certificate) (*Server, error) {
	return start(plan, certificate, "")
}

// StartLoopback starts the same service with listeners restricted to a loopback IP.
func StartLoopback(plan contracts.BrowserTestPlan, certificate tls.Certificate) (*Server, error) {
	address := net.ParseIP(plan.Host)
	if address == nil || !address.IsLoopback() {
		return nil, errors.New("loopback browser test service requires a loopback IP host")
	}
	return start(plan, certificate, address.String())
}

func start(plan contracts.BrowserTestPlan, certificate tls.Certificate, bindAddress string) (*Server, error) {
	if err := plan.Validate(); err != nil {
		return nil, err
	}
	boot := make([]byte, 16)
	if _, err := rand.Read(boot); err != nil {
		return nil, err
	}
	s := &Server{plan: plan, boot: hex.EncodeToString(boot), sessions: map[string]*session{}, slots: make(chan struct{}, 16)}
	tcp, err := net.Listen("tcp", net.JoinHostPort(bindAddress, fmt.Sprint(plan.Port)))
	if err != nil {
		return nil, err
	}
	s.tcp = tcp
	q, err := net.ListenPacket("udp", net.JoinHostPort(bindAddress, fmt.Sprint(plan.Port)))
	if err != nil {
		tcp.Close()
		return nil, err
	}
	s.quic = q
	udp, err := net.ListenPacket("udp", net.JoinHostPort(bindAddress, fmt.Sprint(plan.RTCPort)))
	if err != nil {
		tcp.Close()
		q.Close()
		return nil, err
	}
	mux := ice.NewUDPMuxDefault(ice.UDPMuxParams{UDPConn: udp})
	s.udpMux = mux
	var settings webrtc.SettingEngine
	settings.SetLite(true)
	networkTypes := []webrtc.NetworkType{webrtc.NetworkTypeUDP4, webrtc.NetworkTypeUDP6}
	if bindAddress != "" {
		settings.SetIncludeLoopbackCandidate(true)
		if net.ParseIP(bindAddress).To4() != nil {
			networkTypes = []webrtc.NetworkType{webrtc.NetworkTypeUDP4}
		} else {
			networkTypes = []webrtc.NetworkType{webrtc.NetworkTypeUDP6}
		}
	}
	settings.SetNetworkTypes(networkTypes)
	settings.SetICEUDPMux(mux)
	// The advertised host comes from the administrator's certificate binding.
	addresses, err := net.LookupHost(plan.Host)
	if err != nil {
		tcp.Close()
		q.Close()
		mux.Close()
		return nil, err
	}
	settings.SetNAT1To1IPs(addresses, webrtc.ICECandidateTypeHost)
	s.rtc = webrtc.NewAPI(webrtc.WithSettingEngine(settings))
	tlsConfig := &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS13}
	s.wt = &webtransport.Server{H3: http3.Server{TLSConfig: tlsConfig.Clone(), QUICConfig: &quic.Config{
		MaxIncomingStreams: 2, MaxIncomingUniStreams: 8, HandshakeIdleTimeout: 5 * time.Second,
		MaxIdleTimeout: 10 * time.Second, EnableDatagrams: true,
		Tracer: func(ctx context.Context, perspective logging.Perspective, connID logging.ConnectionID) *logging.ConnectionTracer {
			traceID := ""
			if id, ok := ctx.Value(quic.ConnectionTracingKey).(quic.ConnectionTracingID); ok {
				traceID = fmt.Sprint(id)
			}
			attrs := []any{"operation", "browser.test.measure", "phase", "quic.connection", "quic_connection_id", connID.String(), "connection_trace_id", traceID, "perspective", fmt.Sprint(perspective), "gate_id", plan.GateID, "code.file.path", "internal/browsertest/server.go", "vcs.ref.head.revision", buildinfo.Revision}
			return &logging.ConnectionTracer{
				StartedConnection: func(_, _ net.Addr, _, _ logging.ConnectionID) {
					slog.Info("browser test QUIC connection", append(attrs, "outcome", "started")...)
				},
				ChoseALPN: func(protocol string) {
					slog.Info("browser test QUIC connection", append(attrs, "outcome", "alpn", "protocol", protocol)...)
				},
				NegotiatedVersion: func(version logging.Version, _, _ []logging.Version) {
					slog.Info("browser test QUIC connection", append(attrs, "outcome", "version", "version", fmt.Sprint(version))...)
				},
				ClosedConnection: func(err error) {
					slog.Info("browser test QUIC connection", append(attrs, "outcome", "closed", "reason", quicCloseReason(err))...)
				},
			}
		},
	}, EnableDatagrams: true, Logger: slog.Default()}, CheckOrigin: func(r *http.Request) bool { return r.Header.Get("Origin") == plan.Origin }}
	s.http = &http.Server{Handler: s, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 10 * time.Second, MaxHeaderBytes: 8192, TLSConfig: tlsConfig}
	s.wt.H3.Handler = s
	s.wg.Go(func() { s.serveEnded("HTTPS", s.http.Serve(tls.NewListener(tcp, tlsConfig))) })
	s.wg.Go(func() { s.serveEnded("QUIC", s.wt.Serve(q)) })
	return s, nil
}

func quicCloseReason(err error) string {
	var transport *quic.TransportError
	var application *quic.ApplicationError
	switch {
	case err == nil:
		return "closed"
	case errors.As(err, &transport):
		return fmt.Sprintf("transport_code=0x%x remote=%t", uint64(transport.ErrorCode), transport.Remote)
	case errors.As(err, &application):
		return fmt.Sprintf("application_code=0x%x remote=%t", uint64(application.ErrorCode), application.Remote)
	case errors.Is(err, context.Canceled):
		return "cancelled"
	default:
		// A peer can supply the text of a close error; never persist that text.
		return fmt.Sprintf("%T", err)
	}
}

func (s *Server) serveEnded(protocol string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.failure = protocol + " 测试监听已停止"
	for _, v := range s.sessions {
		v.cancel()
	}
	slog.Error("browser test listener stopped", "protocol", protocol, "error", err)
}

func (s *Server) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	for _, v := range s.sessions {
		v.cancel()
	}
	s.mu.Unlock()
	_ = s.http.Close()
	_ = s.wt.Close()
	_ = s.quic.Close()
	_ = s.udpMux.Close()
	s.wg.Wait()
}

func (s *Server) Sync(tokens []string) error {
	key, err := base64.RawURLEncoding.DecodeString(s.plan.PublicKey)
	if err != nil {
		return err
	}
	wanted := map[string]contracts.BrowserTestClaim{}
	for _, token := range tokens {
		claim, err := contracts.VerifyBrowserTest(token, ed25519.PublicKey(key), time.Now())
		// Persisted sessions from a prior boot are intentionally never reactivated.
		if errors.Is(err, contracts.ErrBrowserTestExpired) {
			continue
		}
		if err != nil {
			return err
		}
		if claim.Boot != s.boot {
			continue
		}
		if claim.GateID != s.plan.GateID {
			return errors.New("test token gate mismatch")
		}
		wanted[claim.ID] = claim
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.failure != "" {
		return errors.New("test service stopped")
	}
	for id, v := range s.sessions {
		if _, ok := wanted[id]; !ok {
			v.cancel()
			delete(s.sessions, id)
		}
	}
	for id, c := range wanted {
		if _, ok := s.sessions[id]; !ok {
			ctx, cancel := context.WithDeadline(context.Background(), c.Expires)
			s.sessions[id] = &session{claim: c, ctx: ctx, cancel: cancel}
		}
	}
	return nil
}

func (s *Server) Observation() contracts.BrowserTestObservation {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := contracts.BrowserTestObservation{Healthy: !s.closed && s.failure == "", Reason: s.failure, Boot: s.boot, ObservedAt: time.Now().UTC(), Sessions: []contracts.BrowserTestUsage{}}
	for id, v := range s.sessions {
		out.Sessions = append(out.Sessions, contracts.BrowserTestUsage{ID: id, Bytes: v.used})
	}
	return out
}

func (s *Server) consume(v *session, n int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v.ctx.Err() != nil || n < 0 || v.reserved+int64(n) > v.claim.Bytes {
		return false
	}
	v.reserved += int64(n)
	return true
}

func (s *Server) measured(v *session, n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v.used += int64(n)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	purpose := browserTestPurpose(r)
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if purpose == "webtransport" {
		token = r.URL.Query().Get("token")
	}
	key, keyErr := base64.RawURLEncoding.DecodeString(s.plan.PublicKey)
	claim, claimErr := contracts.VerifyBrowserTest(token, ed25519.PublicKey(key), time.Now())
	traceID := ""
	if keyErr == nil && (claimErr == nil || errors.Is(claimErr, contracts.ErrBrowserTestExpired)) && claim.Boot == s.boot && claim.GateID == s.plan.GateID && claim.Purpose == purpose {
		traceID = claim.TraceID
	}
	if purpose != "" {
		s.logBrowserRequest(r, purpose, traceID, "received", "")
	} else if r.URL.Path == "/quic" {
		s.logBrowserRequest(r, "webtransport", traceID, "rejected", "method")
	}
	if r.Header.Get("Origin") != s.plan.Origin {
		if purpose != "" {
			s.logBrowserRequest(r, purpose, traceID, "rejected", "origin")
		}
		http.Error(w, "origin rejected", 403)
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", s.plan.Origin)
	w.Header().Set("Vary", "Origin")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method == "OPTIONS" {
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, CONNECT")
		w.WriteHeader(204)
		return
	}
	if purpose == "" {
		http.NotFound(w, r)
		return
	}
	if reason := browserTokenRejection(keyErr, claimErr, claim, s.boot, s.plan.GateID, purpose); reason != "" {
		s.logBrowserRequest(r, purpose, traceID, "rejected", reason)
		http.Error(w, "invalid test token", 403)
		return
	}
	s.mu.Lock()
	v := s.sessions[claim.ID]
	if s.closed || s.failure != "" || v == nil || v.claim != claim || v.ctx.Err() != nil || v.active || purpose != "https" && v.started {
		s.mu.Unlock()
		s.logBrowserRequest(r, purpose, traceID, "rejected", "session")
		http.Error(w, "session unavailable", 409)
		return
	}
	v.active = true
	v.started = true
	s.wg.Add(1)
	s.mu.Unlock()
	s.logBrowserRequest(r, purpose, traceID, "accepted", "")
	defer s.wg.Done()
	select {
	case s.slots <- struct{}{}:
	default:
		s.mu.Lock()
		v.active = false
		s.mu.Unlock()
		http.Error(w, "busy", 429)
		return
	}
	started := time.Now()
	defer func() {
		<-s.slots
		s.mu.Lock()
		v.active = false
		s.mu.Unlock()
		slog.Info("browser test request", "operation", "browser.test.measure", "trace", claim.TraceID, "session_id", claim.ID, "purpose", purpose, "duration_ms", time.Since(started).Milliseconds(), "code.file.path", "internal/browsertest/server.go", "code.function.name", "ServeHTTP", "vcs.ref.head.revision", buildinfo.Revision)
	}()
	switch purpose {
	case "https":
		count := 64
		if r.URL.Path == "/download" {
			count = 4 << 20
		}
		if !s.consume(v, count) {
			http.Error(w, "limit reached", 429)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", fmt.Sprint(count))
		buf := make([]byte, 64<<10)
		if _, err := rand.Read(buf); err != nil {
			return
		}
		ticker := time.NewTicker(4 * time.Millisecond)
		defer ticker.Stop()
		for left := count; left > 0; {
			select {
			case <-v.ctx.Done():
				return
			case <-r.Context().Done():
				return
			case <-ticker.C:
			}
			n := min(left, len(buf))
			written, err := w.Write(buf[:n])
			s.measured(v, written)
			if err != nil {
				return
			}
			left -= written
		}
	case "webrtc":
		s.serveRTC(w, r, v)
	case "webtransport":
		s.serveQUIC(w, r, v)
	}
}

func browserTestPurpose(r *http.Request) string {
	switch {
	case (r.URL.Path == "/latency" || r.URL.Path == "/download") && r.Method == http.MethodGet:
		return "https"
	case r.URL.Path == "/rtc" && r.Method == http.MethodPost:
		return "webrtc"
	case r.URL.Path == "/quic" && r.Method == http.MethodConnect:
		return "webtransport"
	default:
		return ""
	}
}

func (s *Server) logBrowserRequest(r *http.Request, purpose, traceID, outcome, reason string) {
	attrs := []any{"operation", "browser.test.measure", "phase", "gate.request", "outcome", outcome, "reason", reason, "trace_id", traceID, "gate_id", s.plan.GateID, "purpose", purpose, "method", r.Method, "protocol", r.Proto, "path", r.URL.Path, "origin_matches", r.Header.Get("Origin") == s.plan.Origin, "code.file.path", "internal/browsertest/server.go", "code.function.name", "ServeHTTP", "vcs.ref.head.revision", buildinfo.Revision}
	if id, ok := r.Context().Value(quic.ConnectionTracingKey).(quic.ConnectionTracingID); ok {
		attrs = append(attrs, "connection_trace_id", fmt.Sprint(id))
	}
	slog.Info("browser test request stage", attrs...)
}

func browserTokenRejection(keyErr, claimErr error, claim contracts.BrowserTestClaim, boot string, gateID int, purpose string) string {
	if keyErr != nil {
		return "public_key_invalid"
	}
	if errors.Is(claimErr, contracts.ErrBrowserTestExpired) {
		return "token_expired"
	}
	if claimErr != nil {
		return "token_invalid"
	}
	switch {
	case claim.Boot != boot:
		return "boot_mismatch"
	case claim.GateID != gateID:
		return "gate_mismatch"
	case claim.Purpose != purpose:
		return "purpose_mismatch"
	default:
		return ""
	}
}

func (s *Server) serveRTC(w http.ResponseWriter, r *http.Request, v *session) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	if err != nil {
		http.Error(w, "offer too large", 400)
		return
	}
	var offer webrtc.SessionDescription
	if contracts.DecodeStrict(raw, &offer, 64<<10) != nil || offer.Type != webrtc.SDPTypeOffer {
		http.Error(w, "invalid offer", 400)
		return
	}
	// ICE-lite responds to checks from the browser. Never send checks to addresses supplied in SDP.
	lines := strings.Split(offer.SDP, "\n")
	kept := lines[:0]
	for _, line := range lines {
		if !strings.HasPrefix(line, "a=candidate:") {
			kept = append(kept, line)
		}
	}
	offer.SDP = strings.Join(kept, "\n")
	peer, err := s.rtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		http.Error(w, "rtc unavailable", 503)
		return
	}
	ctx, cancel := context.WithTimeout(v.ctx, 15*time.Second)
	done := false
	defer func() {
		if !done {
			cancel()
			peer.Close()
		}
	}()
	peer.OnDataChannel(func(dc *webrtc.DataChannel) {
		dc.OnMessage(func(message webrtc.DataChannelMessage) {
			if len(message.Data) > 1024 || !s.consume(v, 2*len(message.Data)) {
				cancel()
				return
			}
			s.measured(v, len(message.Data))
			var err error
			if message.IsString {
				err = dc.SendText(string(message.Data))
			} else {
				err = dc.Send(message.Data)
			}
			if err != nil {
				cancel()
				return
			}
			s.measured(v, len(message.Data))
		})
	})
	if err = peer.SetRemoteDescription(offer); err != nil {
		http.Error(w, "invalid offer", 400)
		return
	}
	answer, err := peer.CreateAnswer(nil)
	if err != nil {
		http.Error(w, "rtc unavailable", 503)
		return
	}
	gathered := webrtc.GatheringCompletePromise(peer)
	if err = peer.SetLocalDescription(answer); err != nil {
		http.Error(w, "rtc unavailable", 503)
		return
	}
	select {
	case <-gathered:
	case <-ctx.Done():
		http.Error(w, "rtc timeout", 504)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(peer.LocalDescription())
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.wg.Add(1)
	s.mu.Unlock()
	done = true
	// The peer outlives the signaling request, but is bounded by this session's deadline.
	go func() { defer s.wg.Done(); <-ctx.Done(); cancel(); peer.Close() }()
}

func (s *Server) serveQUIC(w http.ResponseWriter, r *http.Request, v *session) {
	s.logBrowserRequest(r, "webtransport", v.claim.TraceID, "upgrade_started", "")
	session, err := s.wt.Upgrade(w, r)
	if err != nil {
		s.logBrowserRequest(r, "webtransport", v.claim.TraceID, "rejected", "upgrade_failed")
		return
	}
	s.logBrowserRequest(r, "webtransport", v.claim.TraceID, "upgraded", "")
	defer session.CloseWithError(0, "")
	ctx, cancel := context.WithTimeout(v.ctx, 15*time.Second)
	defer cancel()
	for {
		data, err := session.ReceiveDatagram(ctx)
		if err != nil {
			return
		}
		if len(data) > 1024 || !s.consume(v, 2*len(data)) {
			return
		}
		s.measured(v, len(data))
		if err = session.SendDatagram(data); err != nil {
			return
		}
		s.measured(v, len(data))
	}
}
