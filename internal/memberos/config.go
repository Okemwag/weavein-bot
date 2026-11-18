package memberos

import (
	"errors"
	"memberos/web/publicsite"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type config struct {
	databaseURL        string
	botToken           string
	botID              int64
	webhookSecret      string
	sessionSecret      string
	address            string
	managedEnabled     bool
	botRecipientAck    string
	managedWorkspaceID string
	publicWebhookURL   string
	botUsername        string
	adminTelegramID    int64
	trustedProxies     []netip.Prefix
	policies           publicsite.Policies
	operator           publicsite.Operator
	saasEnabled        bool
	saasProPrice       int
	saasProLimit       int
}

func loadConfig() (config, error) {
	if err := secretFileError(); err != nil {
		return config{}, err
	}
	c := config{
		databaseURL:        secretEnv("DATABASE_URL"),
		botToken:           secretEnv("TELEGRAM_BOT_TOKEN"),
		webhookSecret:      secretEnv("TELEGRAM_WEBHOOK_SECRET"),
		sessionSecret:      secretEnv("SESSION_SECRET"),
		address:            os.Getenv("LISTEN_ADDR"),
		managedEnabled:     os.Getenv("MANAGED_BILLING_ENABLED") == "true",
		botRecipientAck:    os.Getenv("BOT_STARS_RECIPIENT_ACK"),
		managedWorkspaceID: os.Getenv("MANAGED_BILLING_WORKSPACE_ID"),
		publicWebhookURL:   os.Getenv("PUBLIC_WEBHOOK_URL"),
		botUsername:        os.Getenv("TELEGRAM_BOT_USERNAME"),
		policies: publicsite.Policies{
			Terms:   strings.TrimSpace(os.Getenv("TERMS_URL")),
			Privacy: strings.TrimSpace(os.Getenv("PRIVACY_URL")),
			Refunds: strings.TrimSpace(os.Getenv("REFUND_POLICY_URL")),
		},
	}
	c.operator = publicsite.Operator{
		LegalName: strings.TrimSpace(os.Getenv("OPERATOR_LEGAL_NAME")),
		Email:     strings.TrimSpace(os.Getenv("OPERATOR_CONTACT_EMAIL")),
		Address:   strings.TrimSpace(os.Getenv("OPERATOR_POSTAL_ADDRESS")),
		Country:   strings.TrimSpace(os.Getenv("OPERATOR_COUNTRY")),
	}
	if c.address == "" {
		c.address = ":8080"
	}
	var err error
	c.botID, err = strconv.ParseInt(os.Getenv("TELEGRAM_BOT_ID"), 10, 64)
	if err != nil || c.botID <= 0 {
		return c, errors.New("TELEGRAM_BOT_ID must be a positive integer")
	}
	if c.databaseURL == "" || c.botToken == "" || c.webhookSecret == "" || len(c.sessionSecret) < 32 {
		return c, errors.New("DATABASE_URL, TELEGRAM_BOT_TOKEN, TELEGRAM_WEBHOOK_SECRET and SESSION_SECRET (at least 32 bytes) are required")
	}
	if value := os.Getenv("MEMBEROS_ADMIN_TELEGRAM_ID"); value != "" {
		c.adminTelegramID, err = strconv.ParseInt(value, 10, 64)
		if err != nil || c.adminTelegramID <= 0 {
			return c, errors.New("MEMBEROS_ADMIN_TELEGRAM_ID must be a positive integer")
		}
	}
	for name, value := range map[string]string{"TERMS_URL": c.policies.Terms, "PRIVACY_URL": c.policies.Privacy, "REFUND_POLICY_URL": c.policies.Refunds} {
		if value != "" && !isHTTPSURL(value) {
			return c, errors.New(name + " must be an absolute https URL")
		}
	}
	if c.operator.LegalName != "" || c.operator.Email != "" {
		if c.operator.LegalName == "" || !strings.Contains(c.operator.Email, "@") || c.operator.Country == "" {
			return c, errors.New("OPERATOR_LEGAL_NAME, OPERATOR_CONTACT_EMAIL and OPERATOR_COUNTRY must all be set to publish the built-in policies")
		}
		// The built-in pages are served from this service's public origin.
		origin := strings.TrimRight(strings.TrimSpace(c.publicWebhookURL), "/")
		if isHTTPSURL(origin) {
			if c.policies.Terms == "" {
				c.policies.Terms = origin + "/terms"
			}
			if c.policies.Privacy == "" {
				c.policies.Privacy = origin + "/privacy"
			}
			if c.policies.Refunds == "" {
				c.policies.Refunds = origin + "/refunds"
			}
		}
	}
	c.saasEnabled = os.Getenv("SAAS_BILLING_ENABLED") == "true"
	c.saasProPrice, c.saasProLimit = 750, 5
	if value := os.Getenv("SAAS_PRO_PRICE_STARS"); value != "" {
		c.saasProPrice, err = strconv.Atoi(value)
		if err != nil || c.saasProPrice < 1 || c.saasProPrice > 10000 {
			return c, errors.New("SAAS_PRO_PRICE_STARS must be between 1 and 10000")
		}
	}
	if value := os.Getenv("SAAS_PRO_COMMUNITY_LIMIT"); value != "" {
		c.saasProLimit, err = strconv.Atoi(value)
		if err != nil || c.saasProLimit < 2 || c.saasProLimit > 1000 {
			return c, errors.New("SAAS_PRO_COMMUNITY_LIMIT must be between 2 and 1000")
		}
	}
	if c.saasEnabled && c.managedEnabled {
		// In managed mode the bot belongs to a creator, so Pro fees would be paid to them.
		return c, errors.New("SAAS_BILLING_ENABLED cannot be combined with MANAGED_BILLING_ENABLED: Pro fees must go to a MemberOS-owned bot")
	}
	if c.saasEnabled && !c.policies.Complete() {
		return c, errors.New("SAAS_BILLING_ENABLED requires TERMS_URL, PRIVACY_URL and REFUND_POLICY_URL, or the OPERATOR_* settings with PUBLIC_WEBHOOK_URL")
	}
	// Buyers must be able to read the terms they agree to before a managed charge.
	if c.managedEnabled && !c.policies.Complete() {
		return c, errors.New("MANAGED_BILLING_ENABLED requires TERMS_URL, PRIVACY_URL and REFUND_POLICY_URL")
	}
	for _, value := range strings.Split(os.Getenv("TRUSTED_PROXY_CIDRS"), ",") {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return c, errors.New("TRUSTED_PROXY_CIDRS must be a comma-separated list of CIDR ranges")
		}
		c.trustedProxies = append(c.trustedProxies, prefix)
	}
	return c, nil
}

func isHTTPSURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil
}

// secretEnv returns NAME, or the contents of the file named by NAME_FILE, so
// secrets can come from Docker or Kubernetes secret mounts instead of the
// environment. Rotating a secret means replacing the file and restarting.
func secretEnv(name string) string {
	path := os.Getenv(name + "_FILE")
	if path == "" {
		return os.Getenv(name)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(body), "\r\n")
}

// secretFileError reports a configured secret file that cannot be used, so a
// typo fails loudly instead of looking like a missing variable.
func secretFileError() error {
	for _, name := range []string{"DATABASE_URL", "TELEGRAM_BOT_TOKEN", "TELEGRAM_WEBHOOK_SECRET", "SESSION_SECRET"} {
		path := os.Getenv(name + "_FILE")
		if path == "" {
			continue
		}
		if os.Getenv(name) != "" {
			return errors.New(name + " and " + name + "_FILE are both set; use one")
		}
		if _, err := os.ReadFile(path); err != nil {
			return errors.New(name + "_FILE cannot be read: " + err.Error())
		}
	}
	return nil
}
