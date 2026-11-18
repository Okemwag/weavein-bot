package memberos

import (
	"errors"
	"net/netip"
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
}

func loadConfig() (config, error) {
	c := config{
		databaseURL:        os.Getenv("DATABASE_URL"),
		botToken:           os.Getenv("TELEGRAM_BOT_TOKEN"),
		webhookSecret:      os.Getenv("TELEGRAM_WEBHOOK_SECRET"),
		sessionSecret:      os.Getenv("SESSION_SECRET"),
		address:            os.Getenv("LISTEN_ADDR"),
		managedEnabled:     os.Getenv("MANAGED_BILLING_ENABLED") == "true",
		botRecipientAck:    os.Getenv("BOT_STARS_RECIPIENT_ACK"),
		managedWorkspaceID: os.Getenv("MANAGED_BILLING_WORKSPACE_ID"),
		publicWebhookURL:   os.Getenv("PUBLIC_WEBHOOK_URL"),
		botUsername:        os.Getenv("TELEGRAM_BOT_USERNAME"),
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
