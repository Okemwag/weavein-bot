package memberos

import (
	"fmt"
	"io"
	"memberos/internal/telegram"
	"net/http"
	"strings"
	"testing"
	"time"
)

const proOwner = int64(501)

// proWorld adds a Free workspace with one native community and turns Pro on.
func proWorld(t *testing.T) (*world, string) {
	w := newWorld(t)
	w.a.cfg.saasEnabled, w.a.cfg.saasProPrice, w.a.cfg.saasProLimit = true, 750, 5
	var creator, workspace string
	w.mustScan(`INSERT INTO creators(display_name,support_contact) VALUES('pro','support') RETURNING id`, []any{&creator})
	w.mustScan(`INSERT INTO workspaces(creator_id,name) VALUES($1,'pro') RETURNING id`, []any{&workspace}, creator)
	w.exec(`INSERT INTO workspace_members(workspace_id,telegram_user_id,role) VALUES($1,$2,'owner')`, workspace, proOwner)
	w.exec(`INSERT INTO communities(creator_id,telegram_chat_id,title,native_invite_link,price_stars,status,workspace_id)
		VALUES($1,-200,'Native','https://t.me/+n',50,'published',$2)`, creator, workspace)
	return w, workspace
}

func (w *world) proCheckout(workspace string) string {
	w.t.Helper()
	r := w.request("POST", "/api/v1/creator/workspaces/"+workspace+"/saas/checkout", "", proOwner)
	if r.Code != 201 {
		w.t.Fatalf("Pro checkout: %d %s", r.Code, r.Body.String())
	}
	return w.text(`SELECT invoice_payload FROM saas_checkouts WHERE workspace_id=$1 ORDER BY created_at DESC LIMIT 1`, workspace)
}

func (w *world) proPay(payload, charge string, first bool, until time.Time) string {
	w.t.Helper()
	return w.deliver(w.nextUpdate.Add(1), "message", fmt.Sprintf(`"message":{"date":%d,"from":{"id":%d},"successful_payment":{"currency":"XTR","total_amount":750,"invoice_payload":%q,"subscription_expiration_date":%d,"is_recurring":true,"is_first_recurring":%t,"telegram_payment_charge_id":%q}}`,
		time.Now().Unix(), proOwner, payload, until.Unix(), first, charge))
}

func (w *world) preCheckoutAnswer(payload string, user int64, amount int) string {
	w.t.Helper()
	var answer string
	original := w.a.telegram
	w.a.telegram = telegram.New("fake", &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		_ = r.ParseForm()
		answer = r.Form.Get("ok")
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":true}`)), Header: make(http.Header)}, nil
	})})
	defer func() { w.a.telegram = original }()
	raw := fmt.Sprintf(`{"id":"q","from":{"id":%d},"currency":"XTR","total_amount":%d,"invoice_payload":%q}`, user, amount, payload)
	if err := w.a.answerPreCheckout(w.ctx, fixtureBot, w.nextUpdate.Add(1), []byte(raw)); err != nil {
		w.t.Fatal(err)
	}
	return answer
}

func TestProUpgradeRenewalCancelAndLapse(t *testing.T) {
	w, workspace := proWorld(t)
	payload := w.proCheckout(workspace)
	if got := w.preCheckoutAnswer(payload, proOwner, 750); got != "true" {
		t.Fatalf("valid Pro pre-checkout answered %s", got)
	}
	if got := w.preCheckoutAnswer(payload, proOwner, 1); got != "false" {
		t.Fatal("Pro pre-checkout accepted a different amount")
	}
	if got := w.preCheckoutAnswer(payload, fixtureBuyer, 750); got != "false" {
		t.Fatal("Pro pre-checkout accepted a different payer")
	}
	until := time.Now().Add(30 * 24 * time.Hour)
	if state := w.proPay(payload, "pro-1", true, until); state != "processed" {
		t.Fatalf("first Pro payment: %s", state)
	}
	if plan := w.text(`SELECT plan_code||':'||community_limit FROM saas_accounts WHERE workspace_id=$1`, workspace); plan != "pro:5" {
		t.Fatalf("after payment account is %s", plan)
	}
	if r := w.request("POST", "/api/v1/creator/workspaces/"+workspace+"/saas/checkout", "", proOwner); r.Code != http.StatusConflict {
		t.Fatalf("second Pro checkout: %d", r.Code)
	}
	if got := w.preCheckoutAnswer(payload, proOwner, 750); got != "false" {
		t.Fatal("pre-checkout accepted while Pro is live")
	}
	// A redelivered charge is recorded once.
	w.proPay(payload, "pro-1", true, until)
	renewed := until.Add(30 * 24 * time.Hour)
	if state := w.proPay(payload, "pro-2", false, renewed); state != "processed" {
		t.Fatalf("Pro renewal: %s", state)
	}
	if n := w.count(`SELECT count(*) FROM saas_payments`); n != 2 {
		t.Fatalf("%d Pro payments recorded", n)
	}
	if n := w.count(`SELECT count(*) FROM saas_financial_events WHERE kind='charge' AND workspace_id=$1`, workspace); n != 2 {
		t.Fatalf("%d SaaS ledger charges", n)
	}
	// Creator Stars payments must never reach the SaaS ledger.
	w.subscribe()
	if n := w.count(`SELECT count(*) FROM saas_financial_events`); n != 2 {
		t.Fatalf("community payment leaked into SaaS ledger: %d events", n)
	}
	r := w.request("GET", "/api/v1/creator/workspaces/"+workspace+"/saas", "", proOwner)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"state":"active"`) || !strings.Contains(r.Body.String(), `"you_paid":true`) {
		t.Fatalf("account view: %d %s", r.Code, r.Body.String())
	}
	if r := w.request("POST", "/api/v1/creator/workspaces/"+workspace+"/saas/cancel", "", proOwner); r.Code != 200 || !w.called("editUserStarSubscription") {
		t.Fatalf("cancel Pro: %d %s", r.Code, r.Body.String())
	}
	state := w.deliver(w.nextUpdate.Add(1), "subscription", fmt.Sprintf(`"subscription":{"user":{"id":%d},"invoice_payload":%q,"state":"canceled"}`, proOwner, payload))
	if state != "processed" || w.text(`SELECT state FROM saas_subscriptions`) != "cancel_scheduled" {
		t.Fatalf("Pro cancellation update: %s", state)
	}
	// Within the grace period nothing changes.
	w.exec(`UPDATE saas_subscriptions SET paid_through=now()-interval '3 days'`)
	w.exec(`UPDATE saas_accounts SET pro_paid_through=now()-interval '3 days' WHERE workspace_id=$1`, workspace)
	if err := w.a.runSaaSLifecycle(w.ctx); err != nil {
		t.Fatal(err)
	}
	if plan := w.text(`SELECT plan_code FROM saas_accounts WHERE workspace_id=$1`, workspace); plan != "pro" {
		t.Fatalf("Pro lapsed inside the grace period: %s", plan)
	}
	w.exec(`UPDATE saas_subscriptions SET paid_through=now()-interval '8 days'`)
	w.exec(`UPDATE saas_accounts SET pro_paid_through=now()-interval '8 days' WHERE workspace_id=$1`, workspace)
	if err := w.a.runSaaSLifecycle(w.ctx); err != nil {
		t.Fatal(err)
	}
	if plan := w.text(`SELECT plan_code||':'||community_limit||':'||(lapsed_at IS NOT NULL) FROM saas_accounts WHERE workspace_id=$1`, workspace); plan != "free:1:true" {
		t.Fatalf("after lapse account is %s", plan)
	}
	if n := w.count(`SELECT count(*) FROM communities WHERE workspace_id=$1 AND status='published'`, workspace); n != 1 {
		t.Fatal("lapse changed an existing community")
	}
	if w.text(`SELECT state FROM saas_subscriptions`) != "expired" {
		t.Fatal("ended Pro subscription not expired")
	}
	if n := w.count(`SELECT count(*) FROM audit_events WHERE action='saas_pro_lapsed'`); n != 1 {
		t.Fatalf("%d lapse audit events", n)
	}
	// Upgrading again works after a lapse.
	w.proCheckout(workspace)
}

func TestProRefundReturnsAccountToFree(t *testing.T) {
	w, workspace := proWorld(t)
	payload := w.proCheckout(workspace)
	w.proPay(payload, "pro-r", true, time.Now().Add(30*24*time.Hour))
	payment := w.text(`SELECT id::text FROM saas_payments`)
	if r := w.request("POST", "/api/v1/admin/saas/payments/"+payment+"/refund", `{"reason":"short"}`, fixtureAdmin); r.Code != 400 {
		t.Fatalf("refund without a real reason: %d", r.Code)
	}
	if r := w.request("POST", "/api/v1/admin/saas/payments/"+payment+"/refund", `{"reason":"first payment within seven days"}`, proOwner); r.Code == 200 {
		t.Fatal("non-admin refunded a Pro payment")
	}
	r := w.request("POST", "/api/v1/admin/saas/payments/"+payment+"/refund", `{"reason":"first payment within seven days"}`, fixtureAdmin)
	if r.Code != 200 || !w.called("refundStarPayment") {
		t.Fatalf("admin refund: %d %s", r.Code, r.Body.String())
	}
	if r := w.request("POST", "/api/v1/admin/saas/payments/"+payment+"/refund", `{"reason":"first payment within seven days"}`, fixtureAdmin); r.Code != http.StatusConflict {
		t.Fatalf("second refund submission: %d", r.Code)
	}
	state := w.deliver(w.nextUpdate.Add(1), "message", fmt.Sprintf(`"message":{"date":%d,"from":{"id":%d},"refunded_payment":{"currency":"XTR","total_amount":750,"invoice_payload":%q,"telegram_payment_charge_id":"pro-r"}}`,
		time.Now().Unix(), proOwner, payload))
	if state != "processed" {
		t.Fatalf("Pro refund update: %s", state)
	}
	if plan := w.text(`SELECT plan_code||':'||community_limit FROM saas_accounts WHERE workspace_id=$1`, workspace); plan != "free:1" {
		t.Fatalf("after refund account is %s", plan)
	}
	if n := w.count(`SELECT count(*) FROM saas_financial_events WHERE kind='refund'`); n != 1 {
		t.Fatalf("%d refund ledger events", n)
	}
}

func TestFailedProInvoiceRefusesPaymentAndAudience(t *testing.T) {
	w, workspace := proWorld(t)
	if r := w.request("POST", "/api/v1/creator/workspaces/"+w.workspace+"/saas/checkout", "", fixtureOwner); r.Code != http.StatusConflict {
		t.Fatalf("pilot account offered Pro: %d", r.Code)
	}
	if r := w.request("POST", "/api/v1/creator/workspaces/"+workspace+"/saas/checkout", "", fixtureBuyer); r.Code != http.StatusNotFound {
		t.Fatalf("non-owner Pro checkout: %d", r.Code)
	}
	w.a.cfg.saasEnabled = false
	if r := w.request("POST", "/api/v1/creator/workspaces/"+workspace+"/saas/checkout", "", proOwner); r.Code != http.StatusServiceUnavailable {
		t.Fatalf("Pro checkout while disabled: %d", r.Code)
	}
	// A payment for an invoice that does not match is quarantined, not applied.
	w.a.cfg.saasEnabled = true
	payload := w.proCheckout(workspace)
	state := w.deliver(w.nextUpdate.Add(1), "message", fmt.Sprintf(`"message":{"date":%d,"from":{"id":%d},"successful_payment":{"currency":"XTR","total_amount":1,"invoice_payload":%q,"subscription_expiration_date":%d,"is_recurring":true,"is_first_recurring":true,"telegram_payment_charge_id":"bad"}}`,
		time.Now().Unix(), proOwner, payload, time.Now().Add(720*time.Hour).Unix()))
	if state != "quarantined" || w.text(`SELECT plan_code FROM saas_accounts WHERE workspace_id=$1`, workspace) != "free" {
		t.Fatalf("mismatched Pro payment ended as %s", state)
	}
}

func TestRetentionRemovesOnlyExpiredPersonalData(t *testing.T) {
	w := newWorld(t)
	w.exec(`INSERT INTO telegram_updates(bot_id,update_id,update_type,payload,state,received_at) VALUES
		($1,900001,'message','{"secret":1}','processed',now()-interval '100 days'),
		($1,900002,'message','{"secret":2}','processed',now()-interval '10 days'),
		($1,900003,'message','{"secret":3}','quarantined',now()-interval '100 days')`, fixtureBot)
	w.exec(`INSERT INTO notifications(bot_id,telegram_user_id,dedupe_key,message,status,sent_at) VALUES
		($1,1,'old','hello','sent',now()-interval '100 days'),($1,1,'new','hello','sent',now())`, fixtureBot)
	w.exec(`INSERT INTO creator_inquiries(name,email,message,status,resolved_at,created_at) VALUES
		('Old','a@b.c','message text','resolved',now()-interval '13 months',now()-interval '14 months'),
		('New','a@b.c','message text','open',NULL,now())`)
	w.exec(`INSERT INTO telegram_profiles(telegram_user_id,first_name,verified_at) VALUES
		(9001,'Visitor',now()-interval '13 months'),($1,'Buyer',now()-interval '13 months')`, fixtureBuyer)
	w.subscribe()
	w.a.applyRetention(w.ctx)
	if n := w.count(`SELECT count(*) FROM telegram_updates WHERE payload ? 'redacted'`); n != 1 {
		t.Fatalf("%d updates redacted, want only the old processed one", n)
	}
	if w.text(`SELECT message FROM notifications WHERE dedupe_key='new'`) != "hello" || w.text(`SELECT message FROM notifications WHERE dedupe_key='old'`) != redacted {
		t.Fatal("notification retention wrong")
	}
	if n := w.count(`SELECT count(*) FROM creator_inquiries`); n != 1 {
		t.Fatalf("%d inquiries left", n)
	}
	if n := w.count(`SELECT count(*) FROM telegram_profiles WHERE telegram_user_id=9001`); n != 0 {
		t.Fatal("unlinked visitor profile kept")
	}
	if n := w.count(`SELECT count(*) FROM telegram_profiles WHERE telegram_user_id=$1`, fixtureBuyer); n != 1 {
		t.Fatal("paying member's profile was deleted")
	}
	if n := w.count(`SELECT count(*) FROM payments`); n != 1 {
		t.Fatal("retention touched payment records")
	}
	// Running again changes nothing.
	for step, rows := range w.a.applyRetention(w.ctx) {
		if rows != 0 {
			t.Fatalf("second retention run changed %d rows in %s", rows, step)
		}
	}
}
