package memberos

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"memberos/internal/identity"
	"memberos/internal/store"
	"memberos/internal/telegram"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestManagedPaymentLifecyclePostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("TEST_DATABASE_URL is required in CI")
		}
		t.Skip("set TEST_DATABASE_URL to run PostgreSQL integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	adminDB, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer adminDB.Close()
	schema := fmt.Sprintf("memberos_it_%d", time.Now().UnixNano())
	if _, err := adminDB.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = adminDB.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE") }()
	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.ConnConfig.RuntimeParams["search_path"] = schema
	db, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	var creatorID, communityID string
	if err := db.QueryRow(ctx, `INSERT INTO creators(display_name,support_contact) VALUES('Test creator','support') RETURNING id`).Scan(&creatorID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO creator_users(creator_id,telegram_user_id,role) VALUES($1,99,'owner')`, creatorID); err != nil {
		t.Fatal(err)
	}
	var workspaceID string
	if err := db.QueryRow(ctx, `INSERT INTO workspaces(creator_id,name) VALUES($1,'Test creator') RETURNING id`, creatorID).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO workspace_members(workspace_id,telegram_user_id,role) VALUES($1,99,'owner')`, workspaceID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE saas_accounts SET plan_code='pilot',community_limit=5,
		managed_billing_allowed=true,granted_reason='Integration fixture' WHERE workspace_id=$1`, workspaceID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `INSERT INTO communities(creator_id,telegram_chat_id,title,price_stars,billing_mode,
		managed_join_link,billing_bot_id,status,workspace_id) VALUES($1,$2,'Test',100,'managed_bot','https://t.me/+test',123,'published',$3) RETURNING id`,
		creatorID, -time.Now().UnixNano(), workspaceID).Scan(&communityID); err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf("test-%d", time.Now().UnixNano())
	if _, err := db.Exec(ctx, `INSERT INTO checkout_attempts(community_id,creator_id,telegram_user_id,billing_bot_id,
        invoice_payload,price_stars,expires_at) VALUES($1,$2,42,123,$3,100,now()+interval '30 minutes')`, communityID, creatorID, payload); err != nil {
		t.Fatal(err)
	}
	a := &app{cfg: config{botID: 123, sessionSecret: "test-session-secret-for-integration-123", managedEnabled: true, botRecipientAck: "123"}, db: db}
	var checkoutAnswers []string
	var cancelCharge string
	a.telegram = telegram.New("fake", &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "answerPreCheckoutQuery") {
			_ = req.ParseForm()
			checkoutAnswers = append(checkoutAnswers, req.Form.Get("ok"))
		}
		if strings.Contains(req.URL.Path, "editUserStarSubscription") {
			_ = req.ParseForm()
			cancelCharge = req.Form.Get("telegram_payment_charge_id")
		}
		body := `{"ok":true,"result":true}`
		if strings.Contains(req.URL.Path, "createInvoiceLink") {
			body = `{"ok":true,"result":"https://t.me/$test-invoice"}`
		}
		if strings.Contains(req.URL.Path, "getChatMember") {
			body = `{"ok":true,"result":{"status":"left","user":{"id":42}}}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})})
	request := func(method, path, body string, user int64) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+identity.SignSession(user, a.cfg.sessionSecret, time.Now()))
		w := httptest.NewRecorder()
		a.routes().ServeHTTP(w, r)
		return w
	}
	workspaceResponse := request("POST", "/api/v1/creator/workspaces", `{"name":"Second creator","support_contact":"second@example.com"}`, 43)
	if workspaceResponse.Code != 201 {
		t.Fatalf("workspace create: %d %s", workspaceResponse.Code, workspaceResponse.Body.String())
	}
	var newWorkspace struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(workspaceResponse.Body.Bytes(), &newWorkspace); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE saas_accounts SET plan_code='pilot',community_limit=5,
		managed_billing_allowed=true,granted_reason='Integration fixture' WHERE workspace_id=$1`, newWorkspace.ID); err != nil {
		t.Fatal(err)
	}
	connector := &app{cfg: config{botID: 123, sessionSecret: a.cfg.sessionSecret, managedEnabled: true, botRecipientAck: "123", managedWorkspaceID: newWorkspace.ID, publicWebhookURL: "https://memberos.example"}, db: db}
	botRights := false
	webhookOK := false
	connector.telegram = telegram.New("fake", &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{"ok":true,"result":true}`
		switch {
		case strings.Contains(req.URL.Path, "getMe"):
			body = `{"ok":true,"result":{"id":123,"is_bot":true}}`
		case strings.Contains(req.URL.Path, "getChatMember"):
			if req.URL.Query().Get("user_id") == "43" {
				body = `{"ok":true,"result":{"status":"creator","user":{"id":43}}}`
			} else {
				body = fmt.Sprintf(`{"ok":true,"result":{"status":"administrator","can_invite_users":%t,"can_restrict_members":%t,"user":{"id":123}}}`, botRights, botRights)
			}
		case strings.Contains(req.URL.Path, "getChat"):
			body = `{"ok":true,"result":{"type":"channel"}}`
		case strings.Contains(req.URL.Path, "createChatInviteLink"):
			body = `{"ok":true,"result":{"invite_link":"https://t.me/+connected"}}`
		case strings.Contains(req.URL.Path, "setWebhook"):
			body = fmt.Sprintf(`{"ok":true,"result":%t}`, webhookOK)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})})
	connectRequest := func(path, body string, user int64) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+identity.SignSession(user, connector.cfg.sessionSecret, time.Now()))
		w := httptest.NewRecorder()
		connector.routes().ServeHTTP(w, r)
		return w
	}
	newChatID := -time.Now().UnixNano()
	connectPath := "/api/v1/creator/workspaces/" + newWorkspace.ID + "/communities"
	connectBody := fmt.Sprintf(`{"telegram_chat_id":%d,"title":"New community","description":"Test","price_stars":120,"billing_mode":"managed_bot"}`, newChatID)
	if response := connectRequest(connectPath, connectBody, 42); response.Code != 404 {
		t.Fatalf("foreign workspace connect: %d", response.Code)
	}
	if response := connectRequest(connectPath, connectBody, 43); response.Code != 422 {
		t.Fatalf("missing bot rights: %d %s", response.Code, response.Body.String())
	}
	botRights = true
	connected := connectRequest(connectPath, connectBody, 43)
	if connected.Code != 202 {
		t.Fatalf("draft connection: %d %s", connected.Code, connected.Body.String())
	}
	var connectedCommunity struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(connected.Body.Bytes(), &connectedCommunity); err != nil {
		t.Fatal(err)
	}
	var connectedStatus string
	if err := db.QueryRow(ctx, `SELECT status FROM communities WHERE id=$1`, connectedCommunity.ID).Scan(&connectedStatus); err != nil || connectedStatus != "draft" {
		t.Fatalf("saved community status=%s error=%v", connectedStatus, err)
	}
	recheckPath := "/api/v1/creator/communities/" + connectedCommunity.ID + "/recheck"
	if response := connectRequest(recheckPath, "", 42); response.Code != 404 {
		t.Fatalf("foreign recheck: %d", response.Code)
	}
	webhookOK = true
	if response := connectRequest(recheckPath, "", 43); response.Code != 200 {
		t.Fatalf("recheck: %d %s", response.Code, response.Body.String())
	}
	if err := db.QueryRow(ctx, `SELECT status FROM communities WHERE id=$1`, connectedCommunity.ID).Scan(&connectedStatus); err != nil || connectedStatus != "published" {
		t.Fatalf("published community status=%s error=%v", connectedStatus, err)
	}
	planBody := `{"name":"Premium","price_stars":250,"description":"Research","benefits":"Reports","terms":"30 days recurring; cancellation ends renewal"}`
	if response := request("POST", "/api/v1/creator/communities/"+communityID+"/plans", planBody, 43); response.Code != 404 {
		t.Fatalf("cross-tenant plan create: %d", response.Code)
	}
	planResponse := request("POST", "/api/v1/creator/communities/"+communityID+"/plans", planBody, 99)
	if planResponse.Code != 201 {
		t.Fatalf("plan create: %d %s", planResponse.Code, planResponse.Body.String())
	}
	var createdPlan struct {
		ID        string `json:"id"`
		VersionID string `json:"version_id"`
	}
	if err := json.Unmarshal(planResponse.Body.Bytes(), &createdPlan); err != nil {
		t.Fatal(err)
	}
	checkoutResponse := request("POST", "/api/v1/checkouts", fmt.Sprintf(`{"community_id":%q,"plan_id":%q}`, communityID, createdPlan.ID), 42)
	if checkoutResponse.Code != 201 {
		t.Fatalf("checkout create: %d %s", checkoutResponse.Code, checkoutResponse.Body.String())
	}
	if !strings.Contains(checkoutResponse.Body.String(), `"price_stars":250`) {
		t.Fatal(checkoutResponse.Body.String())
	}
	var snapshot struct {
		CheckoutID string `json:"checkout_id"`
	}
	if err := json.Unmarshal(checkoutResponse.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	var snapPrice int
	var snapVersion string
	if err := db.QueryRow(ctx, `SELECT price_stars,plan_version_id FROM checkout_attempts WHERE id=$1`, snapshot.CheckoutID).Scan(&snapPrice, &snapVersion); err != nil {
		t.Fatal(err)
	}
	if snapPrice != 250 || snapVersion != createdPlan.VersionID {
		t.Fatalf("checkout snapshot price=%d version=%s", snapPrice, snapVersion)
	}
	reviseResponse := request("POST", "/api/v1/creator/plans/"+createdPlan.ID+"/versions", `{"price_stars":300,"terms":"Updated terms"}`, 99)
	if reviseResponse.Code != 201 {
		t.Fatalf("plan revision: %d %s", reviseResponse.Code, reviseResponse.Body.String())
	}
	if response := request("POST", "/api/v1/creator/plans/"+createdPlan.ID+"/versions", `{"price_stars":300,"terms":"Updated terms"}`, 43); response.Code != 404 {
		t.Fatalf("cross-tenant revision: %d", response.Code)
	}
	if err := db.QueryRow(ctx, `SELECT price_stars,plan_version_id FROM checkout_attempts WHERE id=$1`, snapshot.CheckoutID).Scan(&snapPrice, &snapVersion); err != nil {
		t.Fatal(err)
	}
	if snapPrice != 250 || snapVersion != createdPlan.VersionID {
		t.Fatal("plan revision changed existing checkout")
	}
	if response := request("PATCH", "/api/v1/creator/plans/"+createdPlan.ID, `{"status":"paused"}`, 43); response.Code != 404 {
		t.Fatalf("cross-tenant plan status: %d", response.Code)
	}
	if response := request("PATCH", "/api/v1/creator/plans/"+createdPlan.ID, `{"status":"paused"}`, 99); response.Code != 200 {
		t.Fatalf("pause plan: %d %s", response.Code, response.Body.String())
	}
	if response := request("POST", "/api/v1/checkouts", fmt.Sprintf(`{"community_id":%q,"plan_id":%q}`, communityID, createdPlan.ID), 42); response.Code != 404 {
		t.Fatalf("paused plan checkout: %d", response.Code)
	}
	for _, amount := range []int{100, 99} {
		raw, _ := json.Marshal(map[string]any{"id": fmt.Sprintf("query-%d", amount), "from": map[string]any{"id": 42},
			"currency": "XTR", "total_amount": amount, "invoice_payload": payload})
		if err := a.answerPreCheckout(ctx, 123, int64(amount), raw); err != nil {
			t.Fatal(err)
		}
	}
	if len(checkoutAnswers) != 2 || checkoutAnswers[0] != "true" || checkoutAnswers[1] != "false" {
		t.Fatalf("pre-checkout answers: %v", checkoutAnswers)
	}
	baseUpdateID := time.Now().UnixNano()
	now := time.Now().Unix()
	expiration := now + 2592000
	payment := map[string]any{"update_id": int64(1), "message": map[string]any{
		"date": now, "from": map[string]any{"id": 42}, "successful_payment": map[string]any{
			"currency": "XTR", "total_amount": 100, "invoice_payload": payload, "subscription_expiration_date": expiration,
			"is_recurring": true, "is_first_recurring": true, "telegram_payment_charge_id": payload + "-charge",
		},
	}}
	insertUpdate := func(id int64, content map[string]any) {
		t.Helper()
		content["update_id"] = id
		raw, err := json.Marshal(content)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(ctx, `INSERT INTO telegram_updates(bot_id,update_id,update_type,payload) VALUES(123,$1,$2,$3)`,
			id, func() string {
				if _, ok := content["message"]; ok {
					return "message"
				}
				return "subscription"
			}(), raw); err != nil {
			t.Fatal(err)
		}
		more, err := a.processOne(ctx)
		if err != nil || !more {
			t.Fatalf("process update %d: more=%v err=%v", id, more, err)
		}
	}
	insertUpdate(baseUpdateID+1, payment)
	var state string
	var through time.Time
	if err := db.QueryRow(ctx, `SELECT state,paid_through FROM subscriptions WHERE invoice_payload=$1`, payload).Scan(&state, &through); err != nil {
		t.Fatal(err)
	}
	if state != "active" || through.Unix() != expiration {
		t.Fatalf("payment state=%s through=%v", state, through)
	}
	membershipRequest := httptest.NewRequest(http.MethodGet, "/api/v1/me/membership?community_id="+communityID, nil)
	membershipRequest.Header.Set("Authorization", "Bearer "+identity.SignSession(42, a.cfg.sessionSecret, time.Now()))
	membershipResponse := httptest.NewRecorder()
	a.routes().ServeHTTP(membershipResponse, membershipRequest)
	if membershipResponse.Code != 200 || !strings.Contains(membershipResponse.Body.String(), `"entitled":true`) {
		t.Fatalf("membership response=%d body=%s", membershipResponse.Code, membershipResponse.Body.String())
	}
	for _, tc := range []struct {
		user int64
		want int
	}{{99, 200}, {43, 404}} {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/creator/communities/"+communityID+"/members", nil)
		request.Header.Set("Authorization", "Bearer "+identity.SignSession(tc.user, a.cfg.sessionSecret, time.Now()))
		response := httptest.NewRecorder()
		a.routes().ServeHTTP(response, request)
		if response.Code != tc.want {
			t.Fatalf("creator members user %d got %d want %d", tc.user, response.Code, tc.want)
		}
	}
	var subscriptionID string
	if err := db.QueryRow(ctx, `SELECT id FROM subscriptions WHERE invoice_payload=$1`, payload).Scan(&subscriptionID); err != nil {
		t.Fatal(err)
	}
	cancelRequest := httptest.NewRequest(http.MethodPost, "/api/v1/me/subscriptions/"+subscriptionID+"/cancel", nil)
	cancelRequest.Header.Set("Authorization", "Bearer "+identity.SignSession(42, a.cfg.sessionSecret, time.Now()))
	cancelResponse := httptest.NewRecorder()
	a.routes().ServeHTTP(cancelResponse, cancelRequest)
	if cancelResponse.Code != 200 || cancelCharge != payload+"-charge" {
		t.Fatalf("cancel response=%d charge=%s", cancelResponse.Code, cancelCharge)
	}
	cancelUpdate := map[string]any{"subscription": map[string]any{"user": map[string]any{"id": 42}, "invoice_payload": payload, "state": "canceled"}}
	insertUpdate(baseUpdateID+2, cancelUpdate)
	if err := db.QueryRow(ctx, `SELECT state,paid_through FROM subscriptions WHERE invoice_payload=$1`, payload).Scan(&state, &through); err != nil {
		t.Fatal(err)
	}
	if state != "cancel_scheduled" || through.Unix() != expiration {
		t.Fatalf("cancel state=%s through=%v", state, through)
	}
	refund := map[string]any{"message": map[string]any{"date": now + 1, "refunded_payment": map[string]any{
		"currency": "XTR", "total_amount": 100, "invoice_payload": payload, "telegram_payment_charge_id": payload + "-charge",
	}}}
	insertUpdate(baseUpdateID+3, refund)
	var entitlements int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM entitlements WHERE community_id=$1 AND status='active'`, communityID).Scan(&entitlements); err != nil {
		t.Fatal(err)
	}
	if entitlements != 0 {
		t.Fatalf("refunded payment left %d active entitlements", entitlements)
	}
	insertUpdate(baseUpdateID+4, payment)
	var payments int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM payments WHERE invoice_payload=$1`, payload).Scan(&payments); err != nil {
		t.Fatal(err)
	}
	if payments != 1 {
		t.Fatalf("duplicate charge created %d payments", payments)
	}
	banCalls := 0
	a.telegram = telegram.New("fake", &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{"ok":true,"result":true}`
		if strings.Contains(req.URL.Path, "getChatMember") {
			body = `{"ok":true,"result":{"status":"member","user":{"id":42}}}`
		}
		if strings.Contains(req.URL.Path, "banChatMember") {
			banCalls++
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})})
	for i := 0; i < 2; i++ {
		more, err := a.accessService().ProcessOne(ctx)
		if err != nil || !more {
			t.Fatalf("access job %d: more=%v err=%v", i, more, err)
		}
	}
	if banCalls != 1 {
		t.Fatalf("expected one removal call, got %d", banCalls)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMiniAppAssets(t *testing.T) {
	a := &app{}
	for _, path := range []string{"/app/", "/app/app.css", "/app/app.js", "/app/shared.js", "/app/subscriber.js", "/app/member_status.js", "/app/creator.js"} {
		response := httptest.NewRecorder()
		a.routes().ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != 200 {
			t.Fatalf("%s returned %d", path, response.Code)
		}
	}
}
