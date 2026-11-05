package subscriber

import (
	"reflect"
	"testing"
	"time"

	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
)

func TestTicketOutcomeAuditFailureNeverSendsAccessAcceptAndRetryKeepsGrant(t *testing.T) {
	f := newConnectionTestFixture(t)
	if _, err := f.store.sql.Exec(`CREATE TRIGGER synthetic_ticket_outcome_failure BEFORE INSERT ON subscriber_audit WHEN NEW.action='authentication.ticket.authorized' BEGIN SELECT RAISE(ABORT,'synthetic outcome audit failure'); END`); err != nil {
		t.Fatal(err)
	}
	packet := access(f.token)
	if reply := exchange(t, packet, f.address); reply.Code != radius.CodeAccessReject {
		t.Fatal("positive RADIUS response escaped a failed durable verdict", reply.Code)
	}
	at := time.Now()
	before, err := f.store.Snapshot(t.Context(), f.account.ID, at)
	if err != nil || before.LifetimeSeconds != 0 || len(before.Sessions) != 1 || before.Sessions[0].State != "reserved" {
		t.Fatal("failed verdict lost its conservative admission", before, err)
	}
	if status := f.status(t, at); status.Stage != "authorizing" || status.LeaseID != "" {
		t.Fatal("audit failure committed a positive flow state", status)
	}
	if _, err = f.store.sql.Exec(`DROP TRIGGER synthetic_ticket_outcome_failure`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		reply := exchange(t, packet, f.address)
		if reply.Code != radius.CodeAccessAccept || rfc2865.Class_GetString(reply) != before.Sessions[0].ID {
			t.Fatal("same-request recovery issued another grant", reply.Code)
		}
	}
	after, err := f.store.Snapshot(t.Context(), f.account.ID, at)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("outcome retry reset or duplicated the commercial ledger", err)
	}
	var audits int
	if err = f.store.sql.QueryRow(`SELECT count(*) FROM subscriber_audit WHERE action='authentication.ticket.authorized' AND target=?`, Digest(f.token)).Scan(&audits); err != nil || audits != 1 {
		t.Fatal("positive verdict audit missing or duplicated", audits, err)
	}
	if status := f.status(t, time.Now()); status.Stage != "authorized" {
		t.Fatal("successful durable retry has no authorization state", status)
	}
}

func TestTicketIssueAndOwnedFlowRollbackTogether(t *testing.T) {
	f := newConnectionTestFixture(t)
	counts := func() (int, int) {
		t.Helper()
		var tickets, flows int
		if err := f.store.sql.QueryRow(`SELECT count(*) FROM subscriber_tickets`).Scan(&tickets); err != nil {
			t.Fatal(err)
		}
		if err := f.store.sql.QueryRow(`SELECT count(*) FROM subscriber_connection_flows`).Scan(&flows); err != nil {
			t.Fatal(err)
		}
		return tickets, flows
	}
	oldTickets, oldFlows := counts()
	if _, err := f.store.sql.Exec(`CREATE TRIGGER synthetic_ticket_issue_failure BEFORE INSERT ON subscriber_audit WHEN NEW.action='authentication.ticket.requested' BEGIN SELECT RAISE(ABORT,'synthetic ticket audit failure'); END`); err != nil {
		t.Fatal(err)
	}
	if token, err := f.store.IssueTicket(t.Context(), f.input, time.Now()); err == nil || token != "" {
		t.Fatal("partial issue returned a usable credential")
	}
	tickets, flows := counts()
	if tickets != oldTickets || flows != oldFlows {
		t.Fatal("failed audit left an orphan ticket/flow", tickets, flows)
	}
}
