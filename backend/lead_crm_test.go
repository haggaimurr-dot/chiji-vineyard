package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestLeadCRMValidationAndHistoryChanges(t *testing.T) {
	raw := json.RawMessage(`{"leadId":"l-1","expectedVersion":0,"opportunityAmount":123456,"customerProfile":{"email":" buyer@example.com ","industry":"零售","address":"杭州","notes":"每周采购"}}`)
	d, err := decodeLeadUpdate(raw)
	if err != nil {
		t.Fatal(err)
	}
	next, changes, _, err := applyLeadUpdate(Lead{ID: "l-1", Status: "NEW"}, d, "", time.Now())
	if err != nil || next.OpportunityAmount != 123456 || next.CustomerProfile.Email != "buyer@example.com" || len(changes) != 2 {
		t.Fatalf("CRM update: %+v %v %v", next, changes, err)
	}
	_, changes, _, err = applyLeadUpdate(next, d, "", time.Now())
	if err != nil || len(changes) != 0 {
		t.Fatal("unchanged CRM update should not add history")
	}
	for _, raw := range []string{
		`{"leadId":"l-1","expectedVersion":0,"opportunityAmount":-1}`,
		`{"leadId":"l-1","expectedVersion":0,"opportunityAmount":1.23}`,
		`{"leadId":"l-1","expectedVersion":0,"opportunityAmount":100000000001}`,
		`{"leadId":"l-1","expectedVersion":0,"customerProfile":null}`,
		`{"leadId":"l-1","expectedVersion":0,"customerProfile":{"email":"bad"}}`,
		`{"leadId":"l-1","expectedVersion":0,"customerProfile":{"unknown":"bad"}}`,
	} {
		if _, err := decodeLeadUpdate(json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted invalid CRM update %s", raw)
		}
	}
}

func TestLeadCRMPersistenceIsAtomic(t *testing.T) {
	for _, failure := range []bool{false, true} {
		state := &leadTestDB{leads: map[string]Lead{"lead": {ID: "lead", Status: "NEW"}}, historyFail: failure}
		db := sql.OpenDB(leadTestConnector{state})
		server := Server{store: &Store{db: db}}
		_, err := server.updateLead(context.Background(), Claims{Subject: "admin", Username: "运营"}, json.RawMessage(`{"leadId":"lead","expectedVersion":0,"opportunityAmount":12345,"customerProfile":{"email":"buyer@example.com","address":"杭州","industry":"零售","notes":"每周采购"}}`))
		if failure {
			if err == nil || state.leads["lead"].OpportunityAmount != 0 || len(state.history) != 0 {
				t.Fatal("CRM history failure escaped rollback")
			}
		} else if err != nil || state.leads["lead"].OpportunityAmount != 12345 || len(state.history) != 1 || state.history[0].OpportunityAmount != 12345 || state.history[0].CustomerProfile.Email != "buyer@example.com" {
			t.Fatalf("CRM snapshots not persisted: %+v %v", state, err)
		}
		db.Close()
	}
}

type crmFunnelConnector struct{}
type crmFunnelDriver struct{}
type crmFunnelConn struct{ leadListConn }

func (crmFunnelConnector) Connect(context.Context) (driver.Conn, error) { return crmFunnelConn{}, nil }
func (crmFunnelConnector) Driver() driver.Driver                        { return crmFunnelDriver{} }
func (crmFunnelDriver) Open(string) (driver.Conn, error)                { return crmFunnelConn{}, nil }
func (crmFunnelConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if !strings.Contains(q, "GROUP BY status") || strings.Contains(q, "LIMIT") || strings.Contains(q, "status=?") || !strings.Contains(q, "$.opportunityAmount") || len(args) != 1 || args[0].Value != "零售" {
		return nil, errors.New("funnel must aggregate all stages with channel filter before pagination")
	}
	return &leadTestRows{columns: []string{"status", "count", "amount"}, values: [][]driver.Value{{"WON", int64(125), int64(12625)}, {"NEW", int64(100), int64(10100)}}}, nil
}
func TestLeadFunnelAggregatesAllPagesAndStages(t *testing.T) {
	db := sql.OpenDB(crmFunnelConnector{})
	defer db.Close()
	store := Store{db: db}
	result, err := store.leadFunnel(context.Background(), LeadSearch{Channel: "零售", Status: "NEW", Limit: 20, Offset: 100})
	if err != nil || result.Total != 225 || result.Amount != 22725 || result.ActiveAmount != 10100 || result.WonAmount != 12625 || result.WinRate != float64(125)/225 || result.Stages[3].Count != 125 || len(result.Stages) != 5 {
		t.Fatalf("incorrect funnel aggregation: %+v %v", result, err)
	}
}
