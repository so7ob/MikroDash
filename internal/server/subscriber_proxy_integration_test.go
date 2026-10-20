package server

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2869"
	"mikrodash/internal/hub"
	"mikrodash/internal/routeros"
	"mikrodash/internal/session"
	"mikrodash/internal/subscriber"
)

func TestSubscriberHTTPSProxyBindsTicketToVerifiedClientAndRadius(t *testing.T) {
	s, account, _, _ := portalFixture(t)
	cfg := &s.subscribers.cfg
	cfg.Mode = "pilot"
	cfg.LocalLimitsVerified = true
	cfg.AllowDisconnect = true
	cfg.PilotAccounts = []string{account.ID}
	nas := subscriber.NAS{RouterID: "synthetic-router", Network: "C", Address: "127.0.0.1", Secret: "synthetic-proxy-radius-secret", CalledStationID: "hs-lab", LoginURL: "https://hotspot.example.test/login", DisconnectPort: 3799}
	cfg.Clients = []subscriber.NAS{nas}
	service := s.subscribers.radius
	service.Mode = "pilot"
	service.Clients = cfg.Clients
	service.PilotAccounts = map[string]bool{account.ID: true}
	h := hub.New()
	scripted := session.NewForTestWithExec(h, nas.RouterID, func(cmd routeros.Cmd) ([]routeros.Reply, error) {
		if cmd.Path != "/ip/hotspot/host/print" {
			t.Error("unexpected router command", cmd.Path)
			return nil, nil
		}
		return []routeros.Reply{{"address": "127.0.0.1", "mac-address": "02:00:00:00:00:01", "server": "hs-lab"}, {"address": "198.51.100.66", "mac-address": "02:00:00:00:00:66", "server": "hs-lab"}}, nil
	})
	s.sessions = session.NewManagerForTest(nil, h, scripted)
	edge, backend := subscriberProxyFixture(t, s, t.TempDir())
	client := edge.Client()
	client.Timeout = 5 * time.Second
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client.Jar = jar
	send := func(method, path, body, origin, csrf string) *http.Response {
		t.Helper()
		r, err := http.NewRequest(method, edge.URL+path, bytes.NewBufferString(body))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("X-Forwarded-For", "198.51.100.66")
		r.Header.Set("X-Real-IP", "198.51.100.66")
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	decode := func(response *http.Response, code int, v any) {
		t.Helper()
		defer response.Body.Close()
		raw, _ := io.ReadAll(response.Body)
		if response.StatusCode != code {
			t.Fatalf("HTTP %d, expected %d: %s", response.StatusCode, code, raw)
		}
		if v != nil {
			if err := json.Unmarshal(raw, v); err != nil {
				t.Fatal(err, string(raw))
			}
		}
	}
	var login struct {
		CSRF string `json:"csrf"`
	}
	response := send("POST", "/api/login", `{"username":"synthetic-account","password":"synthetic-password"}`, edge.URL, "")
	if response.TLS == nil || len(response.Cookies()) != 1 || !response.Cookies()[0].Secure || !response.Cookies()[0].HttpOnly || response.Cookies()[0].Domain != "" {
		t.Fatal("edge lost TLS or host-cookie boundary")
	}
	decode(response, 200, &login)
	for _, path := range []string{"/api/routers", "/api/users", "/api/subscribers", "/api/subscribers/reconciliation", "/app.js", "/settings"} {
		decode(send("GET", path, "", edge.URL, ""), 404, nil)
	}
	var snapshot subscriber.Snapshot
	decode(send("GET", "/api/account?account=foreign", "", edge.URL, ""), 200, &snapshot)
	if snapshot.Subscription.AccountID != account.ID {
		t.Fatal("portal ownership lost behind proxy", snapshot)
	}
	decode(send("POST", "/api/speed", `{"profileId":"economy"}`, "https://evil.example.test", login.CSRF), 403, nil)
	decode(send("POST", "/api/speed", `{"profileId":"economy"}`, edge.URL, "wrong"), 403, nil)
	decode(send("POST", "/api/connect", `{"network":"C","device":"02:00:00:00:00:66"}`, edge.URL, login.CSRF), 400, nil)
	var ticket struct{ Username, Password, Destination, LoginURL string }
	decode(send("POST", "/api/connect", `{"network":"C"}`, edge.URL, login.CSRF), 200, &ticket)
	if ticket.Username != account.Username || ticket.Destination != edge.URL+"/" || ticket.LoginURL != nas.LoginURL || !strings.HasPrefix(ticket.Password, "mdt_") {
		t.Fatal("unexpected owned ticket destinations")
	}
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	nasServer := &radius.PacketServer{SecretSource: service, Handler: service, ErrorLog: log.New(io.Discard, "", 0)}
	go func() { _ = nasServer.Serve(conn) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = nasServer.Shutdown(ctx)
	})
	access := func(device string) *radius.Packet {
		p := radius.New(radius.CodeAccessRequest, []byte(nas.Secret))
		_ = rfc2865.UserName_SetString(p, ticket.Username)
		_ = rfc2865.UserPassword_SetString(p, ticket.Password)
		_ = rfc2865.CallingStationID_SetString(p, device)
		_ = rfc2865.CalledStationID_SetString(p, nas.CalledStationID)
		_ = rfc2869.MessageAuthenticator_Set(p, make([]byte, 16))
		raw, err := p.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		mac := hmac.New(md5.New, p.Secret)
		_, _ = mac.Write(raw)
		_ = rfc2869.MessageAuthenticator_Set(p, mac.Sum(nil))
		return p
	}
	exchange := func(p *radius.Packet) *radius.Packet {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		out, err := radius.Exchange(ctx, p, conn.LocalAddr().String())
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	if out := exchange(access("02:00:00:00:00:66")); out.Code != radius.CodeAccessReject {
		t.Fatal("browser-injected client claim obtained the ticket", out.Code)
	}
	request := access("02:00:00:00:00:01")
	accepted := exchange(request)
	if accepted.Code != radius.CodeAccessAccept || rfc2865.Class_GetString(accepted) == "" {
		t.Fatal("verified client ticket did not reach actual RADIUS", accepted.Code)
	}
	if retry := exchange(request); rfc2865.Class_GetString(retry) != rfc2865.Class_GetString(accepted) {
		t.Fatal("exact RADIUS retry changed owned lease")
	}
	if replay := exchange(access("02:00:00:00:00:01")); replay.Code != radius.CodeAccessReject {
		t.Fatal("one-use ticket replay was accepted", replay.Code)
	}
	cookies := jar.Cookies(response.Request.URL)
	if len(cookies) != 1 {
		t.Fatal(cookies)
	}
	headers := http.Header{"Origin": []string{edge.URL}, "Cookie": []string{cookies[0].String()}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "wss"+strings.TrimPrefix(edge.URL, "https")+"/ws", &websocket.DialOptions{HTTPClient: client, HTTPHeader: headers})
	if err != nil {
		t.Fatal(err)
	}
	_, frame, err := ws.Read(ctx)
	if err != nil || !bytes.Contains(frame, []byte(account.ID)) {
		ws.CloseNow()
		t.Fatal("owned WebSocket frame missing", err, string(frame))
	}
	if err = ws.Write(ctx, websocket.MessageText, []byte(`{"event":"router:select","data":{"id":"foreign"}}`)); err != nil {
		ws.CloseNow()
		t.Fatal(err)
	}
	if _, _, err = ws.Read(ctx); err == nil {
		ws.CloseNow()
		t.Fatal("portal accepted admin socket traffic")
	}
	ws.CloseNow()
	direct, err := http.NewRequest("GET", backend.URL+"/api/account", nil)
	if err != nil {
		t.Fatal(err)
	}
	direct.Host = response.Request.Host
	direct.Header.Set("X-Forwarded-Proto", "https")
	direct.Header.Set("X-Forwarded-For", "198.51.100.66")
	direct.AddCookie(cookies[0])
	rawBackend, err := http.DefaultClient.Do(direct)
	if err != nil {
		t.Fatal(err)
	}
	decode(rawBackend, 403, nil)
	decode(send("POST", "/api/logout", `{}`, edge.URL, login.CSRF), 200, nil)
	decode(send("GET", "/api/account", "", edge.URL, ""), 401, nil)
}
