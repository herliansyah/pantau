package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"pantau/internal/store"
)

type Dispatcher struct {
	db         *store.DB
	httpClient *http.Client
}

func New(db *store.DB) *Dispatcher {
	return &Dispatcher{
		db: db,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (d *Dispatcher) SendAlert(host *store.Host, rule *store.DesiredRule, summary, rootCause string) {
	tgToken, _ := d.db.GetSetting("telegram_token")
	tgChatID, _ := d.db.GetSetting("telegram_chat_id")
	webhookURL, _ := d.db.GetSetting("webhook_url")

	// 1. Dispatch Telegram
	if tgToken != "" && tgChatID != "" {
		go d.sendTelegram(tgToken, tgChatID, host.Name, summary, rootCause)
	}

	// 2. Dispatch Webhook
	if webhookURL != "" {
		go d.sendWebhook(webhookURL, host, rule, summary, rootCause)
	}
}

func (d *Dispatcher) sendTelegram(token, chatID, hostName, summary, rootCause string) {
	// Trim root cause to fit in Telegram 4096 char limit
	excerpt := strings.TrimSpace(rootCause)
	if len(excerpt) > 1500 {
		excerpt = excerpt[:1500] + "\n...[truncated]"
	}

	var msg strings.Builder
	msg.WriteString("🚨 <b>PANTAU DRIFT ALERT</b>\n\n")
	msg.WriteString(fmt.Sprintf("<b>Host:</b> %s\n", htmlEscape(hostName)))
	msg.WriteString(fmt.Sprintf("<b>Issue:</b> %s\n\n", htmlEscape(summary)))
	if excerpt != "" {
		msg.WriteString("<b>Root Cause Excerpt:</b>\n")
		msg.WriteString(fmt.Sprintf("<pre>%s</pre>", htmlEscape(excerpt)))
	}

	endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	payload := map[string]interface{}{
		"chat_id":    chatID,
		"text":       msg.String(),
		"parse_mode": "HTML",
	}

	body, _ := json.Marshal(payload)
	_, _ = d.httpClient.Post(endpoint, "application/json", bytes.NewReader(body))
}

func (d *Dispatcher) sendWebhook(url string, host *store.Host, rule *store.DesiredRule, summary, rootCause string) {
	payload := map[string]interface{}{
		"event":              "drift_alert",
		"timestamp":          time.Now().UTC().Format(time.RFC3339),
		"host_id":            host.ID,
		"host_name":          host.Name,
		"host_ip":            host.Host,
		"summary":            summary,
		"root_cause_excerpt": rootCause,
	}
	if rule != nil {
		payload["rule_kind"] = rule.Kind
		payload["rule_target"] = rule.Target
		payload["rule_expected"] = rule.Expected
	}

	body, _ := json.Marshal(payload)
	_, _ = d.httpClient.Post(url, "application/json", bytes.NewReader(body))
}

func (d *Dispatcher) TestNotification() error {
	tgToken, _ := d.db.GetSetting("telegram_token")
	tgChatID, _ := d.db.GetSetting("telegram_chat_id")
	webhookURL, _ := d.db.GetSetting("webhook_url")

	if tgToken == "" && webhookURL == "" {
		return fmt.Errorf("no Telegram token or Webhook URL configured")
	}

	var errs []string

	if tgToken != "" && tgChatID != "" {
		endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", tgToken)
		payload := map[string]interface{}{
			"chat_id":    tgChatID,
			"text":       "✅ <b>Pantau Notification Test</b>\nKoneksi notifikasi Telegram berhasil terhubung!",
			"parse_mode": "HTML",
		}
		body, _ := json.Marshal(payload)
		resp, err := d.httpClient.Post(endpoint, "application/json", bytes.NewReader(body))
		if err != nil {
			errs = append(errs, fmt.Sprintf("telegram: %v", err))
		} else if resp.StatusCode >= 300 {
			errs = append(errs, fmt.Sprintf("telegram returned status %d", resp.StatusCode))
		}
	}

	if webhookURL != "" {
		payload := map[string]string{
			"event":   "test_ping",
			"message": "Pantau webhook test notification",
		}
		body, _ := json.Marshal(payload)
		resp, err := d.httpClient.Post(webhookURL, "application/json", bytes.NewReader(body))
		if err != nil {
			errs = append(errs, fmt.Sprintf("webhook: %v", err))
		} else if resp.StatusCode >= 300 {
			errs = append(errs, fmt.Sprintf("webhook returned status %d", resp.StatusCode))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

func htmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
