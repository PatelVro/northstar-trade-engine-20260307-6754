package alerts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/smtp"
	"strings"
	"time"
)

type LogProvider struct{}

func (p *LogProvider) Name() string {
	return "log"
}

func (p *LogProvider) Send(alert Alert) error {
	log.Printf(
		"[alert][%s][%s] trader=%s/%s message=%s",
		alert.Category,
		alert.Event,
		alert.TraderID,
		alert.TraderName,
		alert.Message,
	)
	return nil
}

type WebhookProvider struct {
	URL    string
	Client *http.Client
}

func (p *WebhookProvider) Name() string {
	return "webhook"
}

func (p *WebhookProvider) Send(alert Alert) error {
	url := strings.TrimSpace(p.URL)
	if url == "" {
		return fmt.Errorf("webhook url is empty")
	}
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	body, err := json.Marshal(alert)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %s", resp.Status)
	}
	return nil
}

type EmailProvider struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	To       []string
}

func (p *EmailProvider) Name() string {
	return "email"
}

func (p *EmailProvider) Send(alert Alert) error {
	host := strings.TrimSpace(p.Host)
	from := strings.TrimSpace(p.From)
	if host == "" || p.Port <= 0 || from == "" || len(p.To) == 0 {
		return fmt.Errorf("email provider is not fully configured")
	}
	recipients := make([]string, 0, len(p.To))
	for _, recipient := range p.To {
		recipient = strings.TrimSpace(recipient)
		if recipient == "" {
			continue
		}
		recipients = append(recipients, recipient)
	}
	if len(recipients) == 0 {
		return fmt.Errorf("email recipients are empty")
	}

	auth := smtp.PlainAuth("", p.Username, p.Password, host)
	addr := fmt.Sprintf("%s:%d", host, p.Port)
	subject := fmt.Sprintf("[Northstar][%s] %s", strings.ToUpper(string(alert.Category)), alert.Event)
	message := buildEmailMessage(from, recipients, subject, alert)
	return smtp.SendMail(addr, auth, from, recipients, []byte(message))
}

func buildEmailMessage(from string, to []string, subject string, alert Alert) string {
	lines := []string{
		fmt.Sprintf("From: %s", from),
		fmt.Sprintf("To: %s", strings.Join(to, ", ")),
		fmt.Sprintf("Subject: %s", subject),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		fmt.Sprintf("Category: %s", alert.Category),
		fmt.Sprintf("Event: %s", alert.Event),
		fmt.Sprintf("Trader: %s (%s)", alert.TraderName, alert.TraderID),
		fmt.Sprintf("Time: %s", alert.CreatedAt.Format(time.RFC3339)),
		"",
		alert.Message,
	}
	if len(alert.Metadata) > 0 {
		lines = append(lines, "", "Metadata:")
		keys := make([]string, 0, len(alert.Metadata))
		for key := range alert.Metadata {
			keys = append(keys, key)
		}
		// keep stable for tests/readability
		sortStrings(keys)
		for _, key := range keys {
			lines = append(lines, fmt.Sprintf("- %s: %s", key, alert.Metadata[key]))
		}
	}
	return strings.Join(lines, "\r\n")
}

func sortStrings(values []string) {
	for i := 0; i < len(values)-1; i++ {
		for j := i + 1; j < len(values); j++ {
			if values[j] < values[i] {
				values[i], values[j] = values[j], values[i]
			}
		}
	}
}
