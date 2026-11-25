package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// HomeAssistant delivers alerts through Home Assistant's notify action, to
// a phone its companion app registered: POST
// <URL>/api/services/notify/<Service>, with a long-lived access token.
type HomeAssistant struct {
	URL     string // "http://homeassistant.local:8123"
	Token   string // a long-lived access token (profile → Security)
	Service string // "mobile_app_your_iphone"
	Client  *http.Client
}

// NewHomeAssistant is a notifier for url, token and service, or nil when
// any is unset: alerts are off.
func NewHomeAssistant(url, token, service string) *HomeAssistant {
	if url == "" || token == "" || service == "" {
		return nil
	}
	return &HomeAssistant{URL: strings.TrimRight(url, "/"), Token: token, Service: service,
		Client: &http.Client{Timeout: 15 * time.Second}}
}

// Target names the phone alerts go to.
func (h *HomeAssistant) Target() string { return "Home Assistant · " + h.Service }

// Send posts the alert. A link opens when the notification is tapped, and
// the level sets how it interrupts (iOS).
func (h *HomeAssistant) Send(ctx context.Context, a Alert, link string) error {
	data := map[string]any{}
	if link != "" {
		data["url"] = link
	}
	if a.Level != "" {
		data["push"] = map[string]any{"interruption-level": a.Level}
	}
	body, err := json.Marshal(map[string]any{"title": a.Title, "message": a.Message, "data": data})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.URL+"/api/services/notify/"+h.Service, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+h.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("home assistant answered %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	return nil
}
