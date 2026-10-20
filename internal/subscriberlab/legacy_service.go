package subscriberlab

import (
	"context"
	"io"
	"log"
	"net"
	"os"
	"sync/atomic"
	"time"

	"layeh.com/radius"
	"mikrodash/internal/db"
	"mikrodash/internal/subscriber"
)

// The secondary is a synthetic legacy authority using the actual Go service,
// not User Manager. Its distinct account has a separate durable test ledger.
func legacyAuthority(ctx context.Context, privateDir string, fixture FixtureCredentials) (*subscriber.Store, string, *atomic.Int64, func(), error) {
	dir, err := os.MkdirTemp(privateDir, "legacy-selection-")
	if err != nil {
		return nil, "", nil, nil, err
	}
	database, err := db.Open(dir)
	if err != nil {
		return nil, "", nil, nil, err
	}
	servers := []*radius.PacketServer{}
	closeAll := func() {
		for _, server := range servers {
			stop, cancel := context.WithTimeout(context.Background(), time.Second)
			_ = server.Shutdown(stop)
			cancel()
		}
		_ = database.Close()
	}
	ledger := database.Subscribers()
	account, _, err := ledger.CreateAccount(ctx, "test:subscriber-chr", "legacy-lab", "Synthetic legacy RADIUS selection", fixture.Password, "isolated legacy selection probe", 0)
	if err != nil {
		closeAll()
		return nil, "", nil, nil, err
	}
	at := time.Now()
	plan := subscriber.Plan{ID: "legacy-selection", Version: 1, Name: "Synthetic legacy selection", Start: "first-login", ValidSeconds: 10,
		MaxSessions: 1, Networks: []string{"C"}, Down: 128000, Up: 64000,
		Speeds: []subscriber.Speed{{ID: "bounded", Name: "Bounded", Down: 128000, Up: 64000}},
		Policies: []subscriber.Policy{{ID: "bounds", Limits: []subscriber.Limit{
			{ID: "time", Kind: "seconds", State: "limited", Amount: 10, Action: "disconnect", Period: subscriber.Period{Kind: "none", Zone: "UTC", Anchor: at}},
			{ID: "download", Kind: "download", State: "limited", Amount: 65536, Action: "disconnect", Period: subscriber.Period{Kind: "none", Zone: "UTC", Anchor: at}},
		}}}}
	if err = ledger.SavePlan(ctx, "test:subscriber-chr", "isolated legacy selection", plan); err == nil {
		_, err = ledger.Assign(ctx, "test:subscriber-chr", account.ID, plan.ID, 1, "isolated legacy selection", at)
	}
	if err != nil {
		closeAll()
		return nil, "", nil, nil, err
	}
	service := &subscriber.RadiusService{Store: ledger, Mode: "pilot", PilotAccounts: map[string]bool{account.ID: true}, Clients: []subscriber.NAS{{RouterID: "legacy-selection", Network: "C", Address: "127.0.0.1", CalledStationID: "hs-lab", Secret: fixture.Secret}}}
	count := &atomic.Int64{}
	handler := radius.HandlerFunc(func(w radius.ResponseWriter, r *radius.Request) {
		if r.Packet.Code == radius.CodeAccessRequest {
			count.Add(1)
		}
		service.ServeRADIUS(w, r)
	})
	for _, address := range []string{"127.0.0.1:12814", "127.0.0.1:12815"} {
		conn, err := net.ListenPacket("udp", address)
		if err != nil {
			closeAll()
			return nil, "", nil, nil, err
		}
		server := &radius.PacketServer{Handler: handler, SecretSource: service, ErrorLog: log.New(io.Discard, "", 0)}
		servers = append(servers, server)
		go server.Serve(conn)
	}
	return ledger, account.ID, count, closeAll, nil
}
