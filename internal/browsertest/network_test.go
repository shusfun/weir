package browsertest

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/quic-go/webtransport-go"
	"github.com/shusfun/weir/packages/contracts"
)

func networkFixture(t *testing.T) (*Server, ed25519.PrivateKey, *tls.Config) {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, pub, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	port := func() int {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer l.Close()
		return l.Addr().(*net.TCPAddr).Port
	}
	plan := contracts.BrowserTestPlan{GateID: 1, Port: port(), RTCPort: port(), CertID: 1, Host: "127.0.0.1", Origin: "https://center.example.test", PublicKey: base64.RawURLEncoding.EncodeToString(pub)}
	for plan.Port == plan.RTCPort {
		plan.RTCPort = port()
	}
	s, err := Start(plan, tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	// Loopback candidates are enabled only in this isolated test fixture.
	var settings webrtc.SettingEngine
	settings.SetLite(true)
	settings.SetIncludeLoopbackCandidate(true)
	settings.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	settings.SetICEUDPMux(s.udpMux)
	s.rtc = webrtc.NewAPI(webrtc.WithSettingEngine(settings))
	return s, key, &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13}
}

func TestNetworkProtocolsAndSessionRevocation(t *testing.T) {
	s, key, tlsConfig := networkFixture(t)
	endpoint := fmt.Sprintf("https://127.0.0.1:%d", s.plan.Port)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	tokenFor := func(purpose string) string {
		id := make([]byte, 16)
		if _, err := rand.Read(id); err != nil {
			t.Fatal(err)
		}
		claim := contracts.BrowserTestClaim{ID: fmt.Sprintf("%x", id), Boot: s.boot, GateID: 1, UserID: 1, Purpose: purpose, Bytes: contracts.BrowserTestMaxBytes, Expires: time.Now().Add(time.Minute), TraceID: strings.Repeat("d", 32)}
		token, err := contracts.SignBrowserTest(claim, key)
		if err != nil {
			t.Fatal(err)
		}
		if err = s.Sync([]string{token}); err != nil {
			t.Fatal(err)
		}
		return token
	}
	transport := &http.Transport{TLSClientConfig: tlsConfig}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second}
	request := func(method, path, token string, body []byte) *http.Response {
		req, err := http.NewRequestWithContext(ctx, method, endpoint+path, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", s.plan.Origin)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	token := tokenFor("https")
	for _, path := range []string{"/latency", "/download"} {
		resp := request("GET", path, token, nil)
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		expected := 64
		if path == "/download" {
			expected = 4 << 20
		}
		if err != nil || resp.StatusCode != 200 || len(data) != expected {
			t.Fatalf("%s status=%d bytes=%d error=%v", path, resp.StatusCode, len(data), err)
		}
	}
	if err := s.Sync(nil); err != nil {
		t.Fatal(err)
	}
	resp := request("GET", "/latency", token, nil)
	resp.Body.Close()
	if resp.StatusCode != 409 {
		t.Fatal("revoked HTTPS session accepted")
	}

	token = tokenFor("webrtc")
	var settings webrtc.SettingEngine
	settings.SetIncludeLoopbackCandidate(true)
	settings.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	peer, err := webrtc.NewAPI(webrtc.WithSettingEngine(settings)).NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	dc, err := peer.CreateDataChannel("test", nil)
	if err != nil {
		t.Fatal(err)
	}
	echoed := make(chan string, 1)
	dc.OnOpen(func() {
		if err := dc.SendText("udp-echo"); err != nil {
			select {
			case echoed <- err.Error():
			default:
			}
		}
	})
	dc.OnMessage(func(m webrtc.DataChannelMessage) {
		select {
		case echoed <- string(m.Data):
		default:
		}
	})
	offer, err := peer.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = peer.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(peer.LocalDescription())
	if err != nil {
		t.Fatal(err)
	}
	resp = request("POST", "/rtc", token, raw)
	var answer webrtc.SessionDescription
	err = json.NewDecoder(resp.Body).Decode(&answer)
	resp.Body.Close()
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("RTC answer: %d %v", resp.StatusCode, err)
	}
	if err = peer.SetRemoteDescription(answer); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-echoed:
		if got != "udp-echo" {
			t.Fatal(got)
		}
	case <-ctx.Done():
		t.Fatal("UDP echo timed out", ctx.Err())
	}
	if s.Observation().Sessions[0].Bytes != 16 {
		t.Fatal("RTC payload accounting")
	}
	peer.Close()

	token = tokenFor("webtransport")
	dialer := &webtransport.Dialer{TLSClientConfig: tlsConfig}
	defer dialer.Close()
	_, session, err := dialer.Dial(ctx, endpoint+"/quic?token="+url.QueryEscape(token), http.Header{"Origin": []string{s.plan.Origin}})
	if err != nil {
		t.Fatal(err)
	}
	defer session.CloseWithError(0, "")
	if err = session.SendDatagram([]byte("quic-echo")); err != nil {
		t.Fatal(err)
	}
	data, err := session.ReceiveDatagram(ctx)
	if err != nil || string(data) != "quic-echo" {
		t.Fatal("QUIC echo", string(data), err)
	}
	if err = s.Sync(nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-session.Context().Done():
	case <-ctx.Done():
		t.Fatal("revoked QUIC session did not stop")
	}
}
