package memberos

import (
	"memberos/web/publicsite"
	"strings"
	"testing"
)

func setBaseConfig(t *testing.T) {
	t.Helper()
	for k, v := range map[string]string{
		"DATABASE_URL": "postgres://localhost/x", "TELEGRAM_BOT_TOKEN": "1:abc", "TELEGRAM_BOT_ID": "1",
		"TELEGRAM_WEBHOOK_SECRET": "secret", "SESSION_SECRET": strings.Repeat("s", 32),
		"MANAGED_BILLING_ENABLED": "", "TERMS_URL": "", "PRIVACY_URL": "", "REFUND_POLICY_URL": "",
	} {
		t.Setenv(k, v)
	}
}

func TestManagedBillingRequiresPublishedPolicies(t *testing.T) {
	setBaseConfig(t)
	t.Setenv("MANAGED_BILLING_ENABLED", "true")
	t.Setenv("TERMS_URL", "https://example.com/terms")
	if _, err := loadConfig(); err == nil || !strings.Contains(err.Error(), "REFUND_POLICY_URL") {
		t.Fatalf("expected missing policy error, got %v", err)
	}
	t.Setenv("PRIVACY_URL", "https://example.com/privacy")
	t.Setenv("REFUND_POLICY_URL", "https://example.com/refunds")
	if _, err := loadConfig(); err != nil {
		t.Fatalf("complete policies rejected: %v", err)
	}
}

func TestPolicyURLsMustBeHTTPS(t *testing.T) {
	setBaseConfig(t)
	for _, bad := range []string{"http://example.com/terms", "/terms", "https://user:pw@example.com/t", "javascript:alert(1)"} {
		t.Setenv("TERMS_URL", bad)
		if _, err := loadConfig(); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestNativeModeRunsWithoutPolicies(t *testing.T) {
	setBaseConfig(t)
	if _, err := loadConfig(); err != nil {
		t.Fatal(err)
	}
}

func TestTermsReplyIncludesPolicies(t *testing.T) {
	got := policyLines(publicsiteTestPolicies())
	if !strings.Contains(got, "Refunds: https://example.com/refunds") || !strings.Contains(got, "Terms: https://example.com/terms") {
		t.Fatalf("got %q", got)
	}
}

func publicsiteTestPolicies() publicsite.Policies {
	return publicsite.Policies{Terms: "https://example.com/terms", Privacy: "https://example.com/privacy", Refunds: "https://example.com/refunds"}
}
