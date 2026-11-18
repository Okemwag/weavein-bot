package memberos

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"memberos/internal/identity"
	"memberos/internal/store"
	"memberos/internal/telegram"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	fixtureBot   = int64(123)
	fixtureBuyer = int64(42)
	fixtureOwner = int64(99)
	fixtureAdmin = int64(7)
)

// world is a migrated, isolated schema with one published managed community and
// a scripted Telegram.
type world struct {
	t          *testing.T
	ctx        context.Context
	db         *pgxpool.Pool
	a          *app
	community  string
	creator    string
	workspace  string
	memberKind string
	calls      []string
	failMethod map[string]int
	onCall     func(method string)
	nextUpdate atomic.Int64
}

func newWorld(t *testing.T) *world {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("TEST_DATABASE_URL is required in CI")
		}
		t.Skip("set TEST_DATABASE_URL to run PostgreSQL regression tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	schema := fmt.Sprintf("memberos_rt_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE") })
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	db, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	w := &world{t: t, ctx: ctx, db: db, memberKind: "left", failMethod: map[string]int{}}
	w.a = &app{cfg: fixtureConfig(), db: db}
	w.a.telegram = telegram.New("fake", &http.Client{Transport: roundTripFunc(w.telegramResponse)})
	w.mustScan(`INSERT INTO creators(display_name,support_contact) VALUES('c','support') RETURNING id`, []any{&w.creator})
	w.mustScan(`INSERT INTO workspaces(creator_id,name) VALUES($1,'w') RETURNING id`, []any{&w.workspace}, w.creator)
	w.exec(`INSERT INTO workspace_members(workspace_id,telegram_user_id,role) VALUES($1,$2,'owner')`, w.workspace, fixtureOwner)
	w.exec(`UPDATE saas_accounts SET plan_code='pilot',community_limit=5,managed_billing_allowed=true WHERE workspace_id=$1`, w.workspace)
	w.mustScan(`INSERT INTO communities(creator_id,telegram_chat_id,title,price_stars,billing_mode,managed_join_link,billing_bot_id,status,workspace_id)
		VALUES($1,-100,'Test',100,'managed_bot','https://t.me/+x',$2,'published',$3) RETURNING id`, []any{&w.community}, w.creator, fixtureBot, w.workspace)
	var plan string
	w.mustScan(`INSERT INTO plans(workspace_id,community_id,name) VALUES($1,$2,'Membership') RETURNING id`, []any{&plan}, w.workspace, w.community)
	w.exec(`INSERT INTO plan_versions(plan_id,version,price_stars,terms) VALUES($1,1,100,'terms')`, plan)
	return w
}

func fixtureConfig() config {
	return config{botID: fixtureBot, sessionSecret: strings.Repeat("s", 32), managedEnabled: true,
		botRecipientAck: "123", adminTelegramID: fixtureAdmin, botUsername: "MemberBot"}
}

func (w *world) telegramResponse(r *http.Request) (*http.Response, error) {
	method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	w.calls = append(w.calls, method)
	if w.onCall != nil {
		w.onCall(method)
	}
	respond := func(code int, body string) (*http.Response, error) {
		return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	}
	if w.failMethod[method] != 0 {
		code := w.failMethod[method]
		return respond(code, fmt.Sprintf(`{"ok":false,"error_code":%d,"description":"scripted","parameters":{"retry_after":90}}`, code))
	}
	switch method {
	case "createInvoiceLink":
		return respond(200, `{"ok":true,"result":"https://t.me/$invoice"}`)
	case "getChatMember":
		status := w.memberKind
		if r.URL.Query().Get("user_id") == fmt.Sprint(fixtureBot) {
			status = "administrator"
		}
		return respond(200, fmt.Sprintf(`{"ok":true,"result":{"status":%q,"can_invite_users":true,"can_restrict_members":true,"user":{"id":%s}}}`, status, r.URL.Query().Get("user_id")))
	case "revokeChatInviteLink":
		return respond(200, `{"ok":true,"result":{"invite_link":"https://t.me/+x"}}`)
	case "sendMessage":
		return respond(200, `{"ok":true,"result":{"message_id":1}}`)
	}
	return respond(200, `{"ok":true,"result":true}`)
}

func (w *world) exec(sql string, args ...any) {
	w.t.Helper()
	if _, err := w.db.Exec(w.ctx, sql, args...); err != nil {
		w.t.Fatalf("%s: %v", sql, err)
	}
}

func (w *world) mustScan(sql string, into []any, args ...any) {
	w.t.Helper()
	if err := w.db.QueryRow(w.ctx, sql, args...).Scan(into...); err != nil {
		w.t.Fatalf("%s: %v", sql, err)
	}
}

func (w *world) text(sql string, args ...any) string {
	w.t.Helper()
	var value string
	w.mustScan(sql, []any{&value}, args...)
	return value
}

func (w *world) count(sql string, args ...any) int {
	w.t.Helper()
	var value int
	w.mustScan(sql, []any{&value}, args...)
	return value
}

func (w *world) request(method, path, body string, user int64) *httptest.ResponseRecorder {
	w.t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+identity.SignSession(user, w.a.cfg.sessionSecret, time.Now()))
	response := httptest.NewRecorder()
	w.a.routes().ServeHTTP(response, r)
	return response
}

func (w *world) checkoutPayload() string {
	w.t.Helper()
	response := w.request("POST", "/api/v1/checkouts", fmt.Sprintf(`{"community_id":%q}`, w.community), fixtureBuyer)
	if response.Code != 201 {
		w.t.Fatalf("checkout: %d %s", response.Code, response.Body.String())
	}
	return w.text(`SELECT invoice_payload FROM checkout_attempts ORDER BY created_at DESC LIMIT 1`)
}

// deliver stores an update the way the webhook does and processes it.
func (w *world) deliver(updateID int64, kind, body string) string {
	w.t.Helper()
	raw := fmt.Sprintf(`{"update_id":%d,%s}`, updateID, body)
	w.exec(`INSERT INTO telegram_updates(bot_id,update_id,update_type,payload) VALUES($1,$2,$3,$4)`, fixtureBot, updateID, kind, raw)
	if _, err := w.a.processOne(w.ctx); err != nil {
		w.t.Fatal(err)
	}
	return w.text(`SELECT state FROM telegram_updates WHERE update_id=$1`, updateID)
}

func (w *world) pay(payload, charge string, first bool, until time.Time) {
	w.t.Helper()
	id := w.nextUpdate.Add(1)
	state := w.deliver(id, "message", fmt.Sprintf(`"message":{"date":%d,"from":{"id":%d},"successful_payment":{"currency":"XTR","total_amount":100,"invoice_payload":%q,"subscription_expiration_date":%d,"is_recurring":true,"is_first_recurring":%t,"telegram_payment_charge_id":%q}}`,
		time.Now().Unix(), fixtureBuyer, payload, until.Unix(), first, charge))
	if state != "processed" {
		w.t.Fatalf("payment update ended as %s: %s", state, w.text(`SELECT coalesce(last_error,'') FROM telegram_updates WHERE update_id=$1`, id))
	}
}

// subscribe leaves the buyer with one paid subscription and no queued jobs.
func (w *world) subscribe() string {
	w.t.Helper()
	payload := w.checkoutPayload()
	w.pay(payload, "charge-"+payload, true, time.Now().Add(30*24*time.Hour))
	w.exec(`UPDATE access_jobs SET status='done'`)
	return payload
}

// lapse moves the paid period to have ended the given time ago.
func (w *world) lapse(payload string, ago time.Duration) {
	w.t.Helper()
	until := time.Now().Add(-ago)
	w.exec(`UPDATE subscriptions SET paid_through=$2 WHERE invoice_payload=$1`, payload, until)
	w.exec(`UPDATE payments SET provider_expiration=$2 WHERE invoice_payload=$1`, payload, until)
	w.exec(`UPDATE entitlements e SET valid_until=$2 FROM subscriptions s WHERE s.id=e.subscription_id AND s.invoice_payload=$1`, payload, until)
}

func (w *world) runJobs() {
	w.t.Helper()
	for i := 0; i < 20; i++ {
		more, err := w.a.accessService().ProcessOne(w.ctx)
		if err != nil {
			w.t.Fatal(err)
		}
		if !more {
			return
		}
	}
	w.t.Fatal("access jobs did not settle")
}

func (w *world) called(method string) bool {
	for _, call := range w.calls {
		if call == method {
			return true
		}
	}
	return false
}

func TestSecondSubscriptionIsRefused(t *testing.T) {
	w := newWorld(t)
	pendingInvoice := w.checkoutPayload()
	paid := w.checkoutPayload()
	w.pay(paid, "charge-paid", true, time.Now().Add(30*24*time.Hour))
	if r := w.request("POST", "/api/v1/checkouts", fmt.Sprintf(`{"community_id":%q}`, w.community), fixtureBuyer); r.Code != http.StatusConflict {
		t.Fatalf("checkout while subscribed: %d", r.Code)
	}
	var answers []string
	w.onCall = func(string) {}
	w.calls = nil
	raw := fmt.Sprintf(`{"id":"q","from":{"id":%d},"currency":"XTR","total_amount":100,"invoice_payload":%q}`, fixtureBuyer, pendingInvoice)
	transport := w.a.telegram
	w.a.telegram = telegram.New("fake", &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		_ = r.ParseForm()
		answers = append(answers, r.Form.Get("ok"))
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":true}`)), Header: make(http.Header)}, nil
	})})
	if err := w.a.answerPreCheckout(w.ctx, fixtureBot, 9000, []byte(raw)); err != nil {
		t.Fatal(err)
	}
	w.a.telegram = transport
	if len(answers) != 1 || answers[0] != "false" {
		t.Fatalf("an older open invoice must be refused at pre-checkout: %v", answers)
	}
}

func TestDuplicatePaymentIsRecordedAndStopsRenewing(t *testing.T) {
	w := newWorld(t)
	first := w.checkoutPayload()
	second := w.checkoutPayload()
	w.exec(`INSERT INTO bot_private_contacts(bot_id,telegram_user_id) VALUES($1,$2)`, fixtureBot, fixtureBuyer)
	w.pay(first, "charge-1", true, time.Now().Add(30*24*time.Hour))
	w.pay(second, "charge-2", true, time.Now().Add(30*24*time.Hour))
	if got := w.count(`SELECT count(*) FROM payments`); got != 2 {
		t.Fatalf("the real charge must stay recorded, payments=%d", got)
	}
	w.memberKind = "member"
	w.runJobs()
	if !w.called("editUserStarSubscription") {
		t.Fatal("duplicate renewal was not cancelled at Telegram")
	}
	if got := w.text(`SELECT auto_renew_state FROM subscriptions WHERE invoice_payload=$1`, second); got != "off" {
		t.Fatalf("duplicate renewal state %s", got)
	}
	if got := w.text(`SELECT auto_renew_state FROM subscriptions WHERE invoice_payload=$1`, first); got != "on" {
		t.Fatalf("original renewal must survive, got %s", got)
	}
	if got := w.count(`SELECT count(*) FROM notifications WHERE dedupe_key='duplicate-subscription:charge-2'`); got != 1 {
		t.Fatalf("member was not told about the duplicate, notices=%d", got)
	}
}

func TestLateRenewalNeverRemovesMember(t *testing.T) {
	w := newWorld(t)
	payload := w.subscribe()
	w.lapse(payload, time.Hour)
	w.memberKind = "member"
	if err := w.a.accessService().ExpireDue(w.ctx); err != nil {
		t.Fatal(err)
	}
	if err := w.a.accessService().ReconcileKnown(w.ctx); err != nil {
		t.Fatal(err)
	}
	w.runJobs()
	if w.called("banChatMember") {
		t.Fatal("member with renewal on was removed during the grace window")
	}
	w.pay(payload, "charge-renewal", false, time.Now().Add(30*24*time.Hour))
	w.runJobs()
	if w.called("banChatMember") || w.called("unbanChatMember") {
		t.Fatalf("renewal must not disturb membership: %v", w.calls)
	}
}

func TestExpiryRemovesWhenRenewalCannotArrive(t *testing.T) {
	for name, tc := range map[string]struct {
		ago   time.Duration
		renew string
	}{
		"grace elapsed":  {25 * time.Hour, "on"},
		"renewal off":    {time.Hour, "off"},
		"renewal failed": {time.Hour, "failed"},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			payload := w.subscribe()
			w.lapse(payload, tc.ago)
			w.exec(`UPDATE subscriptions SET auto_renew_state=$2 WHERE invoice_payload=$1`, payload, tc.renew)
			w.memberKind = "member"
			if err := w.a.accessService().ExpireDue(w.ctx); err != nil {
				t.Fatal(err)
			}
			w.runJobs()
			if !w.called("banChatMember") {
				t.Fatalf("expired member not removed: %v", w.calls)
			}
			if got := w.text(`SELECT state FROM subscriptions WHERE invoice_payload=$1`, payload); got != "expired" {
				t.Fatalf("subscription state %s", got)
			}
		})
	}
}

func TestPaymentDuringBanRestoresMember(t *testing.T) {
	w := newWorld(t)
	payload := w.subscribe()
	w.exec(`UPDATE subscriptions SET auto_renew_state='off' WHERE invoice_payload=$1`, payload)
	w.lapse(payload, time.Hour)
	w.memberKind = "member"
	if err := w.a.accessService().ExpireDue(w.ctx); err != nil {
		t.Fatal(err)
	}
	w.onCall = func(method string) {
		if method == "banChatMember" {
			w.onCall = nil
			w.memberKind = "kicked"
			w.exec(`UPDATE entitlements SET status='active',valid_until=now()+interval '30 days'`)
		}
	}
	w.runJobs()
	if !w.called("banChatMember") || !w.called("unbanChatMember") {
		t.Fatalf("ban that raced a payment was not undone: %v", w.calls)
	}
	if got := w.count(`SELECT count(*) FROM notifications WHERE dedupe_key LIKE 'access-restored:%'`); got != 1 {
		t.Fatalf("member was not told how to rejoin, notices=%d", got)
	}
}

func TestInboxClassification(t *testing.T) {
	w := newWorld(t)
	send := func(id int64, message string) (int, string) {
		r := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/telegram/webhooks/%d", fixtureBot), strings.NewReader(fmt.Sprintf(`{"update_id":%d,"message":%s}`, id, message)))
		r.Header.Set("X-Telegram-Bot-Api-Secret-Token", w.a.cfg.webhookSecret)
		response := httptest.NewRecorder()
		w.a.routes().ServeHTTP(response, r)
		if _, err := w.a.processOne(w.ctx); err != nil {
			t.Fatal(err)
		}
		return response.Code, w.text(`SELECT state FROM telegram_updates WHERE update_id=$1`, id)
	}
	w.a.cfg.webhookSecret = "secret"
	private := `{"text":%q,"chat":{"id":55,"type":"private"},"from":{"id":55}}`
	if _, state := send(1, fmt.Sprintf(private, "hello there")); state != "processed" {
		t.Fatalf("private message ended as %s", state)
	}
	if got := w.count(`SELECT count(*) FROM bot_private_contacts WHERE telegram_user_id=55`); got != 1 {
		t.Fatal("a private message must count as contact")
	}
	if strings.Contains(w.text(`SELECT payload::text FROM telegram_updates WHERE update_id=1`), "hello") {
		t.Fatal("private message text must not be stored")
	}
	if _, state := send(2, fmt.Sprintf(private, "/start c_abc")); state != "processed" {
		t.Fatalf("deep link /start ended as %s", state)
	}
	if got := w.count(`SELECT count(*) FROM notifications WHERE telegram_user_id=55`); got != 1 {
		t.Fatal("deep link /start did not get a reply")
	}
	if _, state := send(3, `{"text":"chatter","chat":{"id":-5,"type":"supergroup"},"from":{"id":56}}`); state != "ignored" {
		t.Fatalf("group chatter ended as %s", state)
	}
	if got := w.count(`SELECT count(*) FROM telegram_updates WHERE state IN ('failed','quarantined','answer_unknown')`); got != 0 {
		t.Fatalf("ordinary traffic raised %d alerts", got)
	}
}

func TestOperatorAlertsRepeatOnlyWhenChanged(t *testing.T) {
	w := newWorld(t)
	w.exec(`INSERT INTO telegram_updates(bot_id,update_id,update_type,payload,state) VALUES($1,1,'message','{}','quarantined')`, fixtureBot)
	alerts := func() int { return w.count(`SELECT count(*) FROM notifications WHERE dedupe_key LIKE 'ops-alert:%'`) }
	for i := 0; i < 3; i++ {
		if err := w.a.queueOperationsAlerts(w.ctx); err != nil {
			t.Fatal(err)
		}
	}
	if got := alerts(); got != 1 {
		t.Fatalf("unchanged backlog sent %d alerts", got)
	}
	if got := w.text(`SELECT telegram_user_id::text FROM notifications WHERE dedupe_key LIKE 'ops-alert:%'`); got != "7" {
		t.Fatalf("alert went to %s", got)
	}
	response := w.request("POST", fmt.Sprintf("/api/v1/admin/updates/%d/1/acknowledge", fixtureBot), `{"note":"checked this update by hand"}`, fixtureAdmin)
	if response.Code != 200 {
		t.Fatalf("acknowledge: %d %s", response.Code, response.Body.String())
	}
	if err := w.a.queueOperationsAlerts(w.ctx); err != nil {
		t.Fatal(err)
	}
	if got := alerts(); got != 1 {
		t.Fatalf("acknowledged item still alerts, alerts=%d", got)
	}
}

func TestRandomLowUpdateIDIsNotDropped(t *testing.T) {
	w := newWorld(t)
	payload := w.subscribe()
	w.nextUpdate.Store(900000000)
	w.pay(payload, "charge-high", false, time.Now().Add(60*24*time.Hour))
	state := w.deliver(1000, "subscription", fmt.Sprintf(`"subscription":{"user":{"id":%d},"invoice_payload":%q,"state":"canceled"}`, fixtureBuyer, payload))
	if state != "processed" {
		t.Fatalf("update ended as %s", state)
	}
	if got := w.text(`SELECT auto_renew_state FROM subscriptions WHERE invoice_payload=$1`, payload); got != "off" {
		t.Fatalf("cancellation with a lower update_id was lost: %s", got)
	}
}

func TestPreexistingMemberIsNotRemoved(t *testing.T) {
	w := newWorld(t)
	w.memberKind = "member"
	w.exec(`INSERT INTO access_observations(community_id,telegram_user_id,member_status,observed_at,source) VALUES($1,$2,'member',now(),'chat_member')`, w.community, fixtureBuyer)
	if err := w.a.accessService().ReconcileKnown(w.ctx); err != nil {
		t.Fatal(err)
	}
	w.runJobs()
	if w.called("banChatMember") {
		t.Fatal("member who never paid was banned")
	}
	if r := w.request("GET", "/api/v1/creator/communities/"+w.community+"/access-mismatches", "", fixtureOwner); r.Code != 200 || !strings.Contains(r.Body.String(), `"telegram_user_id":42`) {
		t.Fatalf("creator must still see the mismatch: %d %s", r.Code, r.Body.String())
	}
	w.exec(`INSERT INTO access_overrides(community_id,telegram_user_id,valid_until,reason,created_by,active)
		VALUES($1,$2,now()-interval '1 hour','expired grant',99,false)`, w.community, fixtureBuyer)
	if err := w.a.accessService().ReconcileKnown(w.ctx); err != nil {
		t.Fatal(err)
	}
	w.runJobs()
	if !w.called("banChatMember") {
		t.Fatal("member whose granted access ended must be removed")
	}
}

func TestBrowsingLeavesNoMemberRecords(t *testing.T) {
	w := newWorld(t)
	r := w.request("GET", "/api/v1/me/membership?community_id="+w.community, "", fixtureBuyer)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"access_status":"left"`) {
		t.Fatalf("membership: %d %s", r.Code, r.Body.String())
	}
	if got := w.count(`SELECT (SELECT count(*) FROM members)+(SELECT count(*) FROM access_observations)`); got != 0 {
		t.Fatalf("viewing a community wrote %d rows", got)
	}
	if r := w.request("GET", "/api/v1/me/membership?community_id=nope", "", fixtureBuyer); r.Code != 400 {
		t.Fatalf("malformed id: %d", r.Code)
	}
}

func TestRepeatMembershipChecksReuseObservation(t *testing.T) {
	w := newWorld(t)
	w.memberKind = "member"
	for i := 0; i < 3; i++ {
		if r := w.request("GET", "/api/v1/me/membership?community_id="+w.community, "", fixtureBuyer); r.Code != 200 {
			t.Fatalf("membership: %d", r.Code)
		}
	}
	if len(w.calls) != 1 {
		t.Fatalf("expected one Telegram lookup, got %v", w.calls)
	}
}

func TestRefundThatEndsAccessStopsRenewal(t *testing.T) {
	w := newWorld(t)
	payload := w.subscribe()
	state := w.deliver(5000, "message", fmt.Sprintf(`"message":{"date":%d,"refunded_payment":{"currency":"XTR","total_amount":100,"invoice_payload":%q,"telegram_payment_charge_id":%q}}`, time.Now().Unix(), payload, "charge-"+payload))
	if state != "processed" {
		t.Fatalf("refund ended as %s", state)
	}
	w.memberKind = "member"
	w.runJobs()
	if !w.called("editUserStarSubscription") || !w.called("banChatMember") {
		t.Fatalf("refund must stop renewal and remove access: %v", w.calls)
	}
	if got := w.text(`SELECT state||'/'||auto_renew_state FROM subscriptions WHERE invoice_payload=$1`, payload); got != "refunded/off" {
		t.Fatalf("subscription %s", got)
	}
}

func TestRefundRequestsCanLeaveEveryOpenState(t *testing.T) {
	w := newWorld(t)
	w.subscribe()
	payment := w.text(`SELECT id FROM payments LIMIT 1`)
	request := func() string {
		r := w.request("POST", "/api/v1/me/refund-requests", fmt.Sprintf(`{"payment_id":%q,"reason":"changed my mind today"}`, payment), fixtureBuyer)
		if r.Code != 201 {
			t.Fatalf("refund request: %d %s", r.Code, r.Body.String())
		}
		return w.text(`SELECT id FROM refund_requests WHERE status='requested'`)
	}
	note := `{"note":"reviewed against Stars history"}`
	id := request()
	if r := w.request("POST", "/api/v1/admin/refund-requests/"+id+"/reject", note, fixtureAdmin); r.Code != 200 {
		t.Fatalf("reject: %d %s", r.Code, r.Body.String())
	}
	id = request()
	if r := w.request("POST", "/api/v1/admin/refund-requests/"+id+"/reopen", note, fixtureAdmin); r.Code != http.StatusConflict {
		t.Fatalf("only uncertain requests can be reopened: %d", r.Code)
	}
	w.failMethod["refundStarPayment"] = 500
	if r := w.request("POST", "/api/v1/admin/refund-requests/"+id+"/approve", "", fixtureAdmin); r.Code != http.StatusBadGateway {
		t.Fatalf("approve during outage: %d", r.Code)
	}
	if got := w.text(`SELECT status FROM refund_requests WHERE id=$1`, id); got != "needs_review" {
		t.Fatalf("status %s", got)
	}
	if r := w.request("POST", "/api/v1/admin/refund-requests/"+id+"/reopen", note, fixtureAdmin); r.Code != 200 {
		t.Fatalf("reopen: %d %s", r.Code, r.Body.String())
	}
	delete(w.failMethod, "refundStarPayment")
	if r := w.request("POST", "/api/v1/admin/refund-requests/"+id+"/approve", "", fixtureAdmin); r.Code != 200 {
		t.Fatalf("approve: %d %s", r.Code, r.Body.String())
	}
	if got := w.count(`SELECT count(*) FROM audit_events WHERE action='refund_approval'`); got != 2 {
		t.Fatalf("each refund attempt must be audited, got %d", got)
	}
}

func TestDisconnectIgnoresUnpaidCheckoutsAndCanBeRetried(t *testing.T) {
	w := newWorld(t)
	w.checkoutPayload()
	path := "/api/v1/creator/communities/" + w.community + "/disconnect"
	w.failMethod["revokeChatInviteLink"] = 500
	if r := w.request("POST", path, "", fixtureOwner); r.Code != http.StatusBadGateway {
		t.Fatalf("disconnect during outage: %d %s", r.Code, r.Body.String())
	}
	if got := w.text(`SELECT status FROM communities WHERE id=$1`, w.community); got != "archived" {
		t.Fatalf("community must already be closed, got %s", got)
	}
	delete(w.failMethod, "revokeChatInviteLink")
	if r := w.request("POST", path, "", fixtureOwner); r.Code != 200 {
		t.Fatalf("retry: %d %s", r.Code, r.Body.String())
	}
	if got := w.count(`SELECT count(*) FROM communities WHERE join_link_revoked_at IS NOT NULL`); got != 1 {
		t.Fatal("revocation not recorded")
	}
	if r := w.request("POST", path, "", fixtureOwner); r.Code != 404 {
		t.Fatalf("finished disconnect must not repeat: %d", r.Code)
	}
}

func TestDisconnectStillRefusesPaidCommunity(t *testing.T) {
	w := newWorld(t)
	w.subscribe()
	if r := w.request("POST", "/api/v1/creator/communities/"+w.community+"/disconnect", "", fixtureOwner); r.Code != http.StatusConflict {
		t.Fatalf("paid community disconnect: %d", r.Code)
	}
}

func TestNoConnectionHeldDuringTelegramCalls(t *testing.T) {
	w := newWorld(t)
	var worst int32
	w.onCall = func(string) {
		if held := w.db.Stat().AcquiredConns(); held > worst {
			worst = held
		}
	}
	payload := w.subscribe()
	subscription := w.text(`SELECT id FROM subscriptions WHERE invoice_payload=$1`, payload)
	if r := w.request("POST", "/api/v1/me/subscriptions/"+subscription+"/cancel", "", fixtureBuyer); r.Code != 200 {
		t.Fatalf("cancel: %d %s", r.Code, r.Body.String())
	}
	w.lapse(payload, time.Hour)
	w.memberKind = "member"
	if err := w.a.accessService().ExpireDue(w.ctx); err != nil {
		t.Fatal(err)
	}
	w.runJobs()
	if !w.called("banChatMember") || !w.called("editUserStarSubscription") {
		t.Fatalf("expected Telegram calls: %v", w.calls)
	}
	if worst != 0 {
		t.Fatalf("a database connection was held during a Telegram call (%d)", worst)
	}
}

func TestRateLimitedJobKeepsItsAttempts(t *testing.T) {
	w := newWorld(t)
	payload := w.subscribe()
	w.exec(`UPDATE subscriptions SET auto_renew_state='off' WHERE invoice_payload=$1`, payload)
	w.lapse(payload, time.Hour)
	w.memberKind = "member"
	if err := w.a.accessService().ExpireDue(w.ctx); err != nil {
		t.Fatal(err)
	}
	w.failMethod["banChatMember"] = 429
	if _, err := w.a.accessService().ProcessOne(w.ctx); err != nil {
		t.Fatal(err)
	}
	var attempts int
	var wait float64
	w.mustScan(`SELECT attempts,extract(epoch FROM run_after-now()) FROM access_jobs WHERE action='remove' AND status='pending'`, []any{&attempts, &wait})
	if attempts != 0 || wait < 60 {
		t.Fatalf("rate limit attempts=%d wait=%.0f; Telegram asked for 90 seconds", attempts, wait)
	}
}

func TestInterruptedJobIsPickedUpAgain(t *testing.T) {
	w := newWorld(t)
	payload := w.subscribe()
	w.exec(`UPDATE subscriptions SET auto_renew_state='off' WHERE invoice_payload=$1`, payload)
	w.lapse(payload, time.Hour)
	w.memberKind = "member"
	if err := w.a.accessService().ExpireDue(w.ctx); err != nil {
		t.Fatal(err)
	}
	w.exec(`UPDATE access_jobs SET status='running',claimed_at=now()-interval '10 minutes' WHERE action='remove'`)
	w.runJobs()
	if !w.called("banChatMember") {
		t.Fatal("stale running job was not recovered")
	}
}

// TestPreviouslyBrokenActions drives the handlers that could not run before their
// audit statements were given explicit parameter types.
func TestPreviouslyBrokenActions(t *testing.T) {
	w := newWorld(t)
	payload := w.subscribe()
	subscription := w.text(`SELECT id FROM subscriptions WHERE invoice_payload=$1`, payload)
	community := "/api/v1/creator/communities/" + w.community
	note := `{"resolution":"handled this for the member"}`
	var ticket, inquiry string
	ok := func(name string, r *httptest.ResponseRecorder) {
		t.Helper()
		if r.Code < 200 || r.Code >= 300 {
			t.Fatalf("%s: %d %s", name, r.Code, r.Body.String())
		}
	}
	state := w.deliver(7000, "chat_member", fmt.Sprintf(`"chat_member":{"chat":{"id":-100},"date":%d,"new_chat_member":{"status":"member","user":{"id":%d}}}`, time.Now().Unix(), fixtureBuyer))
	if state != "processed" {
		t.Fatalf("chat_member ended as %s", state)
	}
	ok("onboarding", w.request("PATCH", community+"/onboarding", `{"rules":"be kind","content_guide":"start here","category":"news"}`, fixtureOwner))
	ok("grant override", w.request("POST", community+"/access-overrides", fmt.Sprintf(`{"telegram_user_id":77,"valid_until":%q,"reason":"comped by the creator"}`, time.Now().Add(48*time.Hour).Format(time.RFC3339)), fixtureOwner))
	override := w.text(`SELECT id FROM access_overrides LIMIT 1`)
	ok("revoke override", w.request("POST", community+"/access-overrides/"+override+"/revoke", "", fixtureOwner))
	ok("member note", w.request("POST", fmt.Sprintf("%s/members/%d/notes", community, fixtureBuyer), `{"body":"long time subscriber"}`, fixtureOwner))
	ok("workspace settings", w.request("PATCH", "/api/v1/creator/workspaces/"+w.workspace+"/settings", `{"timezone":"UTC","language":"en","notifications_enabled":true}`, fixtureOwner))
	ok("support ticket", w.request("POST", "/api/v1/me/support-tickets", fmt.Sprintf(`{"community_id":%q,"kind":"access","description":"cannot find the channel"}`, w.community), fixtureBuyer))
	ticket = w.text(`SELECT id FROM support_tickets LIMIT 1`)
	ok("creator resolve", w.request("POST", community+"/support-tickets/"+ticket+"/resolve", note, fixtureOwner))
	ok("second ticket", w.request("POST", "/api/v1/me/support-tickets", fmt.Sprintf(`{"community_id":%q,"kind":"access","description":"another access question"}`, w.community), fixtureBuyer))
	ticket = w.text(`SELECT id FROM support_tickets WHERE status='open'`)
	ok("escalate", w.request("POST", "/api/v1/admin/support-tickets/"+ticket+"/escalate", `{"reason":"needs the operator to look"}`, fixtureAdmin))
	ok("evidence", w.request("GET", "/api/v1/admin/support-tickets/"+ticket+"/evidence", "", fixtureAdmin))
	ok("admin resolve", w.request("POST", "/api/v1/admin/support-tickets/"+ticket+"/resolve", note, fixtureAdmin))
	ok("inquiry", w.request("POST", "/api/v1/creator-inquiries", `{"name":"Sam","email":"sam@example.com","message":"please onboard my community"}`, 0))
	inquiry = w.text(`SELECT id FROM creator_inquiries LIMIT 1`)
	ok("resolve inquiry", w.request("POST", "/api/v1/admin/creator-inquiries/"+inquiry+"/resolve", note, fixtureAdmin))
	ok("saas change", w.request("PATCH", "/api/v1/admin/workspaces/"+w.workspace+"/saas", `{"plan":"pilot","status":"active","community_limit":6,"managed_billing_allowed":true,"reason":"pilot extended this month"}`, fixtureAdmin))
	w.exec(`INSERT INTO telegram_updates(bot_id,update_id,update_type,payload,state) VALUES($1,8000,'message','{}','failed')`, fixtureBot)
	ok("retry update", w.request("POST", fmt.Sprintf("/api/v1/admin/updates/%d/8000/retry", fixtureBot), "", fixtureAdmin))
	w.exec(`UPDATE access_jobs SET status='failed' WHERE dedupe_key=(SELECT dedupe_key FROM access_jobs LIMIT 1)`)
	job := w.text(`SELECT id FROM access_jobs WHERE status='failed' LIMIT 1`)
	ok("retry job", w.request("POST", "/api/v1/admin/access-jobs/"+job+"/retry", "", fixtureAdmin))
	w.exec(`INSERT INTO notifications(bot_id,telegram_user_id,dedupe_key,message,status) VALUES($1,$2,'n1','m','failed'),($1,$2,'n2','m','uncertain')`, fixtureBot, fixtureBuyer)
	notice := w.text(`SELECT id FROM notifications WHERE dedupe_key='n1'`)
	ok("retry notice", w.request("POST", "/api/v1/admin/notifications/"+notice+"/retry", "", fixtureAdmin))
	ok("bulk retry", w.request("POST", "/api/v1/admin/notifications/retry-uncertain?acknowledge_duplicate_risk=true", "", fixtureAdmin))
	if got := w.count(`SELECT count(*) FROM notifications WHERE status='uncertain'`); got != 0 {
		t.Fatalf("uncertain notices left: %d", got)
	}
	w.exec(`INSERT INTO access_overrides(community_id,telegram_user_id,valid_until,reason,created_by) VALUES($1,88,now()-interval '1 minute','grant has ended',99)`, w.community)
	if err := w.a.accessService().ExpireOverrides(w.ctx); err != nil {
		t.Fatalf("override expiry: %v", err)
	}
	ok("cancel", w.request("POST", "/api/v1/me/subscriptions/"+subscription+"/cancel", "", fixtureBuyer))
	if got := w.text(`SELECT auto_renew_state FROM subscriptions WHERE id=$1`, subscription); got != "off" {
		t.Fatalf("cancel not recorded: %s", got)
	}
	ok("deletion request", w.request("POST", "/api/v1/creator/workspaces/"+w.workspace+"/deletion-request", "", fixtureOwner))
}
