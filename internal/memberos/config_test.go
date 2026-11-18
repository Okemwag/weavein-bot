package memberos

import (
	"memberos/web/publicsite"
	"os"
	"strings"
	"testing"
)

func setBaseConfig(t *testing.T) {
	t.Helper()
	for k, v := range map[string]string{
		"DATABASE_URL": "postgres://localhost/x", "TELEGRAM_BOT_TOKEN": "1:abc", "TELEGRAM_BOT_ID": "1",
		"TELEGRAM_WEBHOOK_SECRET": "secret", "SESSION_SECRET": strings.Repeat("s", 32),
		"MANAGED_BILLING_ENABLED": "", "TERMS_URL": "", "PRIVACY_URL": "", "REFUND_POLICY_URL": "",
		"SAAS_BILLING_ENABLED": "", "SAAS_PRO_PRICE_STARS": "", "SAAS_PRO_COMMUNITY_LIMIT": "", "PUBLIC_WEBHOOK_URL": "",
		"OPERATOR_LEGAL_NAME": "", "OPERATOR_CONTACT_EMAIL": "", "OPERATOR_COUNTRY": "", "OPERATOR_POSTAL_ADDRESS": "",
		"DATABASE_URL_FILE": "", "TELEGRAM_BOT_TOKEN_FILE": "", "TELEGRAM_WEBHOOK_SECRET_FILE": "", "SESSION_SECRET_FILE": "",
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

func TestSecretsCanComeFromFiles(t *testing.T) {
	setBaseConfig(t)
	path := t.TempDir() + "/token"
	if err := os.WriteFile(path, []byte("1:from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	t.Setenv("TELEGRAM_BOT_TOKEN_FILE", path)
	c, err := loadConfig()
	if err != nil || c.botToken != "1:from-file" {
		t.Fatalf("token %q err %v", c.botToken, err)
	}
	t.Setenv("TELEGRAM_BOT_TOKEN", "1:abc")
	if _, err := loadConfig(); err == nil {
		t.Fatal("accepted both TELEGRAM_BOT_TOKEN and its _FILE")
	}
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	t.Setenv("TELEGRAM_BOT_TOKEN_FILE", path+".missing")
	if _, err := loadConfig(); err == nil {
		t.Fatal("accepted an unreadable secret file")
	}
}

func TestSaaSBillingConfiguration(t *testing.T) {
	setBaseConfig(t)
	t.Setenv("SAAS_BILLING_ENABLED", "true")
	if _, err := loadConfig(); err == nil {
		t.Fatal("Pro billing enabled without policies")
	}
	t.Setenv("OPERATOR_LEGAL_NAME", "Example Ltd")
	t.Setenv("OPERATOR_CONTACT_EMAIL", "help@example.com")
	t.Setenv("OPERATOR_COUNTRY", "Kenya")
	t.Setenv("PUBLIC_WEBHOOK_URL", "https://memberos.example.com/")
	c, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if c.policies.Privacy != "https://memberos.example.com/privacy" || c.saasProPrice != 750 || c.saasProLimit != 5 {
		t.Fatalf("unexpected defaults %+v %d %d", c.policies, c.saasProPrice, c.saasProLimit)
	}
	t.Setenv("MANAGED_BILLING_ENABLED", "true")
	if _, err := loadConfig(); err == nil || !strings.Contains(err.Error(), "MemberOS-owned bot") {
		t.Fatalf("Pro and managed billing accepted together: %v", err)
	}
	t.Setenv("MANAGED_BILLING_ENABLED", "")
	t.Setenv("SAAS_PRO_PRICE_STARS", "10001")
	if _, err := loadConfig(); err == nil {
		t.Fatal("accepted a Pro price above Telegram's limit")
	}
}
