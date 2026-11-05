package subscriber

import (
	"context"
	"net"
	"testing"
	"time"

	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
)

func TestPortalTicketRejectsAuthenticatedDifferentNASWithoutConsumption(t *testing.T) {
	s, _, account, address := protocolFixture(t, func(service *RadiusService) {
		service.Clients = append(service.Clients, NAS{RouterID: "synthetic-other-router", Network: "C", Address: "127.0.0.2", CalledStationID: "hs-other", Secret: testSecret})
	})
	token, err := s.IssueTicket(t.Context(), TicketRequest{AccountID: account.ID, RouterID: "lab", Network: "C", Device: "02:00:00:00:00:01", ClientAddress: "192.0.2.1"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	wrong := access(token)
	if err = rfc2865.CalledStationID_SetString(wrong, "hs-other"); err != nil {
		t.Fatal(err)
	}
	if err = signResponse(wrong); err != nil {
		t.Fatal(err)
	}
	client := radius.Client{Dialer: net.Dialer{LocalAddr: &net.UDPAddr{IP: net.ParseIP("127.0.0.2")}}}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	reply, err := client.Exchange(ctx, wrong, address)
	if err != nil {
		t.Fatal(err)
	}
	if reply.Code != radius.CodeAccessReject {
		t.Fatal("ticket accepted by another authenticated NAS", reply.Code)
	}
	valid := access(token)
	first := exchange(t, valid, address)
	if first.Code != radius.CodeAccessAccept {
		t.Fatal("wrong NAS consumed the ticket")
	}
	repeated := exchange(t, valid, address)
	if repeated.Code != radius.CodeAccessAccept || rfc2865.Class_GetString(first) != rfc2865.Class_GetString(repeated) {
		t.Fatal("same-request ticket retry changed its authorization")
	}
}
