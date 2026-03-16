package trader

import (
	"northstar/alerts"
	"os"
	"strconv"
	"strings"
)

func newAlertManager(config AutoTraderConfig) *alerts.Manager {
	providers := []alerts.Provider{&alerts.LogProvider{}}

	if webhookURL := strings.TrimSpace(os.Getenv("NORTHSTAR_ALERT_WEBHOOK_URL")); webhookURL != "" {
		providers = append(providers, &alerts.WebhookProvider{URL: webhookURL})
	}

	emailTo := splitCSV(os.Getenv("NORTHSTAR_ALERT_EMAIL_TO"))
	smtpHost := strings.TrimSpace(os.Getenv("NORTHSTAR_ALERT_SMTP_HOST"))
	smtpPort, _ := strconv.Atoi(strings.TrimSpace(os.Getenv("NORTHSTAR_ALERT_SMTP_PORT")))
	emailFrom := strings.TrimSpace(os.Getenv("NORTHSTAR_ALERT_EMAIL_FROM"))
	if len(emailTo) > 0 && smtpHost != "" && smtpPort > 0 && emailFrom != "" {
		providers = append(providers, &alerts.EmailProvider{
			Host:     smtpHost,
			Port:     smtpPort,
			Username: strings.TrimSpace(os.Getenv("NORTHSTAR_ALERT_SMTP_USERNAME")),
			Password: os.Getenv("NORTHSTAR_ALERT_SMTP_PASSWORD"),
			From:     emailFrom,
			To:       emailTo,
		})
	}

	return alerts.NewManager(providers...)
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}
