package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"
	"testing"
	"time"
)

type reservationDB struct {
	queue          map[string]int64
	pendingQueue   map[string]int64
	nextQueue      int64
	readOnly       bool
	home           HomeConfig
	records        map[string]bool
	pending        map[string]bool
	failInsert     bool
	locked         bool
	notices        map[string]ActivityNotice
	pendingNotices map[string]ActivityNotice
	failNotice     bool
}
type reservationConnector struct{ state *reservationDB }
type reservationDriver struct{ state *reservationDB }

func (c reservationConnector) Connect(context.Context) (driver.Conn, error) { return c.state, nil }
func (c reservationConnector) Driver() driver.Driver                        { return reservationDriver{c.state} }
func (d reservationDriver) Open(string) (driver.Conn, error)                { return d.state, nil }
func (d *reservationDB) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (d *reservationDB) Close() error { return nil }
func (d *reservationDB) BeginTx(_ context.Context, opts driver.TxOptions) (driver.Tx, error) {
	d.readOnly = opts.ReadOnly
	return d.Begin()
}
func (d *reservationDB) Begin() (driver.Tx, error) {
	d.pending = map[string]bool{}
	for key, value := range d.records {
		d.pending[key] = value
	}
	d.pendingQueue = map[string]int64{}
	for k, v := range d.queue {
		d.pendingQueue[k] = v
	}
	d.locked = false
	d.pendingNotices = map[string]ActivityNotice{}
	for k, v := range d.notices {
		d.pendingNotices[k] = v
	}
	return d, nil
}
func (d *reservationDB) Commit() error {
	d.queue = d.pendingQueue
	d.pendingQueue = nil
	d.records = d.pending
	d.notices = d.pendingNotices
	d.pending = nil
	d.pendingNotices = nil
	return nil
}
func (d *reservationDB) Rollback() error {
	d.pending = nil
	d.pendingNotices = nil
	d.pendingQueue = nil
	return nil
}

type reservationRows struct {
	values  [][]driver.Value
	config  bool
	columns []string
}

func (r *reservationRows) Columns() []string {
	if r.columns != nil {
		return r.columns
	}
	if r.config {
		return []string{"payload"}
	}
	return []string{"activity_id"}
}
func (r *reservationRows) Close() error { return nil }
func (r *reservationRows) Next(dest []driver.Value) error {
	if len(r.values) == 0 {
		return io.EOF
	}
	copy(dest, r.values[0])
	r.values = r.values[1:]
	return nil
}
func (d *reservationDB) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(query, "FROM activity_notices") {
		user := args[0].Value.(string)
		if strings.Contains(query, "COUNT(*)") {
			count := int64(0)
			for key, n := range d.notices {
				if strings.HasPrefix(key, user+":") && !n.Read {
					count++
				}
			}
			return &reservationRows{columns: []string{"count"}, values: [][]driver.Value{{count}}}, nil
		}
		rows := &reservationRows{columns: []string{"id", "activity_id", "title", "start_date", "end_date", "created_at", "is_read", "kind"}}
		for key, n := range d.notices {
			if strings.HasPrefix(key, user+":") {
				rows.values = append(rows.values, []driver.Value{n.ID, n.ActivityID, n.Title, n.StartDate, n.EndDate, time.Now(), n.Read, n.Kind})
			}
		}
		return rows, nil
	}
	if strings.Contains(query, "GROUP BY activity_id") || strings.Contains(query, "GROUP BY w.activity_id") {
		user := args[0].Value.(string)
		counts := map[string]int64{}
		owns := map[string]int64{}
		positions := map[string]int64{}
		if strings.Contains(query, "activity_waitlist") {
			for key, id := range d.pendingQueue {
				parts := strings.SplitN(key, ":", 2)
				activity := parts[1]
				counts[activity]++
				if parts[0] == user {
					owns[activity] = 1
					for other, otherID := range d.pendingQueue {
						if strings.HasSuffix(other, ":"+activity) && otherID <= id {
							positions[activity]++
						}
					}
				}
			}
		} else {
			for key := range d.pending {
				parts := strings.SplitN(key, ":", 2)
				counts[parts[1]]++
				if parts[0] == user {
					owns[parts[1]] = 1
				}
			}
		}
		rows := &reservationRows{columns: []string{"activity_id", "count", "own"}}
		waiting := strings.Contains(query, "activity_waitlist")
		if waiting {
			rows.columns = append(rows.columns, "position")
		}
		for id, count := range counts {
			v := []driver.Value{id, count, owns[id]}
			if waiting {
				v = append(v, positions[id])
			}
			rows.values = append(rows.values, v)
		}
		return rows, nil
	}
	if strings.Contains(query, "FROM configs") {
		if (!d.readOnly && !strings.Contains(query, "FOR UPDATE")) || d.pending == nil {
			return nil, errors.New("config must be locked in transaction")
		}
		d.locked = true
		return &reservationRows{config: true, values: [][]driver.Value{{[]byte(jsonValue(d.home))}}}, nil
	}
	if strings.Contains(query, "COUNT(*) FROM activity_reservations") || strings.Contains(query, "COUNT(*) FROM activity_waitlist") {
		count := int64(0)
		if strings.Contains(query, "WHERE openid=?") {
			key := args[0].Value.(string) + ":" + args[1].Value.(string)
			if strings.Contains(query, "activity_waitlist") {
				if d.pendingQueue[key] > 0 {
					count = 1
				}
			} else if d.pending[key] {
				count = 1
			}
		} else {
			suffix := ":" + args[0].Value.(string)
			for key := range d.pending {
				if strings.HasSuffix(key, suffix) {
					count++
				}
			}
		}
		return &reservationRows{columns: []string{"count"}, values: [][]driver.Value{{count}}}, nil
	}
	if strings.Contains(query, "SELECT openid FROM activity_waitlist") {
		suffix := ":" + args[0].Value.(string)
		keys := []string{}
		for key := range d.pendingQueue {
			if strings.HasSuffix(key, suffix) {
				keys = append(keys, key)
			}
		}
		sort.Slice(keys, func(i, j int) bool { return d.pendingQueue[keys[i]] < d.pendingQueue[keys[j]] })
		rows := &reservationRows{columns: []string{"openid"}}
		if len(keys) > 0 {
			rows.values = [][]driver.Value{{strings.TrimSuffix(keys[0], suffix)}}
		}
		return rows, nil
	}
	if strings.Contains(query, "FROM activity_reservations WHERE openid=?") {
		rows := &reservationRows{}
		prefix := args[0].Value.(string) + ":"
		for key := range d.records {
			if strings.HasPrefix(key, prefix) {
				rows.values = append(rows.values, []driver.Value{strings.TrimPrefix(key, prefix)})
			}
		}
		return rows, nil
	}
	return nil, errors.New("unexpected query")
}
func (d *reservationDB) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if strings.HasPrefix(query, "INSERT INTO activity_notices") {
		if d.failNotice {
			return nil, errors.New("notice unavailable")
		}
		if d.pendingNotices == nil {
			return nil, errors.New("notice requires transaction")
		}
		n := ActivityNotice{ID: args[0].Value.(string), ActivityID: args[2].Value.(string), Title: args[3].Value.(string), StartDate: args[4].Value.(string), EndDate: args[5].Value.(string), Kind: args[6].Value.(string)}
		d.pendingNotices[args[1].Value.(string)+":"+n.ID] = n
		return driver.RowsAffected(1), nil
	}
	if strings.HasPrefix(query, "UPDATE activity_notices") {
		key := args[1].Value.(string) + ":" + args[0].Value.(string)
		if n, ok := d.notices[key]; ok {
			n.Read = true
			d.notices[key] = n
		}
		return driver.RowsAffected(1), nil
	}
	key := args[0].Value.(string) + ":" + args[1].Value.(string)
	if strings.HasPrefix(query, "INSERT INTO activity_waitlist") {
		d.nextQueue++
		d.pendingQueue[key] = d.nextQueue
		return driver.RowsAffected(1), nil
	}
	if strings.HasPrefix(query, "DELETE FROM activity_waitlist") {
		delete(d.pendingQueue, key)
		return driver.RowsAffected(1), nil
	}
	if strings.HasPrefix(query, "INSERT IGNORE INTO activity_reservations") {
		if d.failInsert {
			return nil, errors.New("insert unavailable")
		}
		if !d.locked || d.pending == nil {
			return nil, errors.New("insert requires current config validation")
		}
		if d.pending[key] {
			return driver.RowsAffected(0), nil
		}
		d.pending[key] = true
	} else if strings.HasPrefix(query, "DELETE FROM activity_reservations WHERE openid=? AND activity_id=?") {
		delete(d.pending, key)
	} else {
		return nil, errors.New("unexpected exec")
	}
	return driver.RowsAffected(1), nil
}

func TestActivityReservationPersistence(t *testing.T) {
	state := &reservationDB{home: defaultHome(), records: map[string]bool{}}
	state.home.Activities = []HomeActivity{{ID: "event", ReservationEnabled: true, StartDate: "2099-01-01", EndDate: "2099-01-31"}}
	db := sql.OpenDB(reservationConnector{state})
	defer db.Close()
	store := &Store{db: db}
	ctx := context.Background()
	set := func(user string, enabled bool) error {
		_, err := store.setActivityReservation(ctx, user, json.RawMessage(jsonValue(M{"activityId": "event", "enabled": enabled})))
		return err
	}
	for _, user := range []string{"alice", "alice", "bob"} {
		if err := set(user, true); err != nil {
			t.Fatal(err)
		}
	}
	if len(state.records) != 2 {
		t.Fatal("duplicate reservation or wrong user isolation")
	}
	if len(state.notices) != 2 {
		t.Fatal("duplicate request generated another notice")
	}
	ids, err := store.activityReservations(ctx, "alice")
	if err != nil || len(ids) != 1 || ids[0] != "event" {
		t.Fatalf("list: %v %v", ids, err)
	}
	state.home.Activities = nil
	if err := set("alice", true); err == nil {
		t.Fatal("removed event accepted")
	}
	if err := set("alice", false); err != nil {
		t.Fatal(err)
	}
	if state.records["alice:event"] || !state.records["bob:event"] {
		t.Fatal("cancellation affected another user")
	}
	if err := set("alice", false); err != nil {
		t.Fatal("cancellation must be idempotent", err)
	}
	state.home.Activities = []HomeActivity{{ID: "event", ReservationEnabled: true, StartDate: "2099-01-01", EndDate: "2099-01-31"}}
	state.failInsert = true
	if err := set("alice", true); err == nil {
		t.Fatal("insert failure not reported")
	}
	if state.records["alice:event"] {
		t.Fatal("failed insert committed")
	}
	_, err = store.setActivityReservation(ctx, "alice", json.RawMessage(`{"activityId":"event"}`))
	if err == nil {
		t.Fatal("missing enabled accepted")
	}
}

func TestReservableActivity(t *testing.T) {
	h := defaultHome()
	h.Activities = []HomeActivity{{ID: "autumn", ReservationEnabled: true, Title: "秋日活动", Summary: "简介", Content: "详情", StartDate: "2026-10-06", EndDate: "2026-10-07"}}
	if err := validateHome(h); err != nil {
		t.Fatal(err)
	}
	// Registration is available ahead of the event, through its last day in China.
	for _, date := range []string{"2026-10-01T00:00:00Z", "2026-10-07T15:59:59Z"} {
		now, _ := time.Parse(time.RFC3339, date)
		if err := reservableActivity(h, "autumn", now); err != nil {
			t.Fatal(err)
		}
	}
	now, _ := time.Parse(time.RFC3339, "2026-10-07T16:00:00Z")
	if reservableActivity(h, "autumn", now) == nil {
		t.Fatal("ended event accepted after China midnight")
	}
	h.Activities[0].ReservationEnabled = false
	if reservableActivity(h, "autumn", now.Add(-24*time.Hour)) == nil {
		t.Fatal("disabled event accepted")
	}
	if reservableActivity(h, "missing", now) == nil {
		t.Fatal("missing event accepted")
	}
	h.Activities[0].ReservationEnabled = true
	h.Activities[0].ID = ""
	if validateHome(h) == nil {
		t.Fatal("reservable activity requires stable ID")
	}
	h.Activities[0].ID = "bad/id"
	if validateHome(h) == nil {
		t.Fatal("invalid activity ID accepted")
	}
	h.Activities[0].ID = "autumn"
	h.Activities = append(h.Activities, h.Activities[0])
	if validateHome(h) == nil {
		t.Fatal("duplicate activity IDs accepted")
	}
}

func TestActivityNoticeTransactionAndOwnership(t *testing.T) {
	state := &reservationDB{home: defaultHome(), records: map[string]bool{}}
	state.home.Activities = []HomeActivity{{ID: "event", ReservationEnabled: true, Title: "活动原名", StartDate: "2099-01-01", EndDate: "2099-01-31"}}
	db := sql.OpenDB(reservationConnector{state})
	defer db.Close()
	store := &Store{db: db}
	ctx := context.Background()
	set := func(enabled bool) error {
		_, err := store.setActivityReservation(ctx, "alice", json.RawMessage(jsonValue(M{"activityId": "event", "enabled": enabled})))
		return err
	}
	state.failNotice = true
	if err := set(true); err == nil {
		t.Fatal("notice insert failure ignored")
	}
	if len(state.records) > 0 || len(state.notices) > 0 {
		t.Fatal("reservation committed without success notice")
	}
	state.failNotice = false
	if err := set(true); err != nil {
		t.Fatal(err)
	}
	state.home.Activities[0].Title = "修改后的标题"
	if err := set(true); err != nil {
		t.Fatal(err)
	}
	result, err := store.activityNotices(ctx, "alice", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	data := result.(M)
	items := data["items"].([]ActivityNotice)
	if len(items) != 1 || items[0].Title != "活动原名" || data["unread"] != 1 {
		t.Fatalf("snapshot/idempotency: %v", data)
	}
	foreign, err := store.activityNotices(ctx, "bob", json.RawMessage(`{}`))
	if err != nil || len(foreign.(M)["items"].([]ActivityNotice)) != 0 {
		t.Fatal("notice leaked to another user")
	}
	_, err = store.readActivityNotice(ctx, "bob", json.RawMessage(jsonValue(M{"id": items[0].ID})))
	if err != nil {
		t.Fatal(err)
	}
	result, _ = store.activityNotices(ctx, "alice", json.RawMessage(`{}`))
	if result.(M)["unread"] != 1 {
		t.Fatal("other user marked notice read")
	}
	_, err = store.readActivityNotice(ctx, "alice", json.RawMessage(jsonValue(M{"id": items[0].ID})))
	if err != nil {
		t.Fatal(err)
	}
	result, _ = store.activityNotices(ctx, "alice", json.RawMessage(`{}`))
	if result.(M)["unread"] != 0 {
		t.Fatal("read did not persist")
	}
	if err := set(false); err != nil {
		t.Fatal(err)
	}
	if len(state.notices) != 1 {
		t.Fatal("cancellation deleted history")
	}
	if err := set(true); err != nil {
		t.Fatal(err)
	}
	if len(state.notices) != 2 {
		t.Fatal("new reservation after cancellation needs new notice")
	}
	for _, raw := range []string{`{"offset":-1}`, `{"limit":51}`} {
		if _, err := store.activityNotices(ctx, "alice", json.RawMessage(raw)); err == nil {
			t.Fatal("invalid pagination accepted")
		}
	}
}

func TestActivityCapacityWaitlistAndPromotion(t *testing.T) {
	state := &reservationDB{home: defaultHome(), records: map[string]bool{}}
	a := HomeActivity{ID: "event", Title: "活动", Summary: "简介", Content: "正文", ReservationEnabled: true, Capacity: 1, WaitlistEnabled: true, StartDate: "2099-01-01", EndDate: "2099-01-31"}
	state.home.Activities = []HomeActivity{a}
	db := sql.OpenDB(reservationConnector{state})
	defer db.Close()
	store := &Store{db: db}
	ctx := context.Background()
	set := func(user string, on bool) (any, error) {
		return store.setActivityReservation(ctx, user, json.RawMessage(jsonValue(M{"activityId": "event", "enabled": on})))
	}
	assertStatus := func(user string, on bool, want string) {
		t.Helper()
		v, e := set(user, on)
		if e != nil || v.(M)["status"] != want {
			t.Fatalf("%s: %v %v want %s", user, v, e, want)
		}
	}
	assertStatus("alice", true, "RESERVED")
	assertStatus("bob", true, "WAITLISTED")
	assertStatus("charlie", true, "WAITLISTED")
	assertStatus("bob", true, "WAITLISTED")
	if len(state.records) != 1 || len(state.queue) != 2 || len(state.notices) != 3 {
		t.Fatal("overbooking or duplicate waitlist notice")
	}
	status, e := store.activityReservationStatus(ctx, "charlie")
	if e != nil {
		t.Fatal(e)
	}
	item := status.(M)["items"].([]ActivityReservationState)[0]
	if item.Status != "WAITLISTED" || item.Position != 2 || item.Confirmed != 1 || item.Waiting != 2 {
		t.Fatalf("queue status: %+v", item)
	}
	assertStatus("bob", false, "NONE")
	assertStatus("bob", true, "WAITLISTED")
	// Cancellation and recursive promotion must both roll back if its notice fails.
	state.failNotice = true
	if _, e = set("alice", false); e == nil {
		t.Fatal("promotion notice failure ignored")
	}
	if !state.records["alice:event"] || len(state.queue) != 2 {
		t.Fatal("cancellation/promotion not atomic")
	}
	state.failNotice = false
	assertStatus("alice", false, "NONE")
	if !state.records["charlie:event"] || state.records["bob:event"] || len(state.queue) != 1 {
		t.Fatal("FIFO promotion or cancel/rejoin order failed")
	}
	promoted := 0
	for key, n := range state.notices {
		if strings.HasPrefix(key, "charlie:") && n.Kind == "PROMOTED" {
			promoted++
		}
	}
	if promoted != 1 {
		t.Fatal("missing promotion notice")
	}
	assertStatus("alice", false, "NONE")
	// Raising capacity by configuration save, or returning to unlimited, promotes old candidates.
	old := []byte(jsonValue(state.home))
	state.home.Activities[0].Capacity = 0
	tx, e := db.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	var payload []byte
	if e = tx.QueryRowContext(ctx, "SELECT payload FROM configs WHERE config_key='home' FOR UPDATE").Scan(&payload); e != nil {
		t.Fatal(e)
	}
	if e = reconcileHomeActivities(ctx, tx, "home", old, state.home); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	if !state.records["bob:event"] || len(state.queue) != 0 {
		t.Fatal("unlimited capacity did not promote queue")
	}
	// Reducing capacity preserves existing confirmations but blocks new admissions.
	state.home.Activities[0].Capacity = 1
	state.home.Activities[0].WaitlistEnabled = false
	if _, e = set("dave", true); e == nil {
		t.Fatal("full activity without waitlist accepted")
	}
	if len(state.records) != 2 {
		t.Fatal("capacity reduction evicted existing reservations")
	}
	state.home.Activities[0].WaitlistEnabled = true
	assertStatus("dave", true, "WAITLISTED")
	state.home.Activities[0].EndDate = "2000-01-31"
	assertStatus("bob", false, "NONE")
	if state.records["dave:event"] || len(state.queue) != 1 {
		t.Fatal("ended activity promoted candidate")
	}
	state.home.Activities = nil
	assertStatus("dave", false, "NONE")
	assertStatus("charlie", false, "NONE")
	if len(state.records) != 0 || len(state.queue) != 0 || len(state.notices) == 0 {
		t.Fatal("removed activity cancellation/history failed")
	}
	for _, capacity := range []int{-1, 100001} {
		state.home.Activities = []HomeActivity{a}
		state.home.Activities[0].Capacity = capacity
		if validateHome(state.home) == nil {
			t.Fatal("invalid capacity accepted")
		}
	}
}
