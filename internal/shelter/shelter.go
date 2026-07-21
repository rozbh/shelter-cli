package shelter

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"shelter-cli/internal/dns"
	"shelter-cli/internal/logging"
)

var panelDomains = []string{
	"https://panel.sheltertm.com",
	"https://panel2.sheltertm.com",
	"https://panel3.sheltertm.com",
}

const (
	fallbackDNS1 = "8.8.8.8"
	fallbackDNS2 = "1.1.1.1"
)

var csrfRe = regexp.MustCompile(`const\s+csrf\s*=\s*"([^"]+)"`)

type sessionData struct {
	XSRFToken      string
	LaravelSession string
	CSRF           string
}

func short(s string) string {
	if len(s) <= 10 {
		return s
	}
	return s[:10] + "..."
}

func fetchOnePanel(client *http.Client, target string) (sessionData, error) {
	var sess sessionData

	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return sess, err
	}

	resp, err := client.Do(req)
	if err != nil {
		logging.Logf("shelter: GET %s failed: %v", target, err)
		return sess, err
	}
	defer resp.Body.Close()

	logging.Logf("shelter: GET %s -> status %d", target, resp.StatusCode)

	if resp.StatusCode >= 400 {
		return sess, fmt.Errorf("status %d from %s", resp.StatusCode, target)
	}

	for _, c := range resp.Cookies() {
		switch c.Name {
		case "XSRF-TOKEN":
			sess.XSRFToken = c.Value
		case "laravel_session":
			sess.LaravelSession = c.Value
		}
	}
	logging.Logf("shelter: cookies found — XSRF-TOKEN present=%v laravel_session present=%v", sess.XSRFToken != "", sess.LaravelSession != "")
	if sess.XSRFToken == "" || sess.LaravelSession == "" {
		return sess, fmt.Errorf("missing cookies from %s (xsrf=%q session=%q)", target, sess.XSRFToken, sess.LaravelSession)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return sess, err
	}

	m := csrfRe.FindSubmatch(body)
	if len(m) != 2 {
		logging.Logf("shelter: csrf regex did not match body from %s (body length %d bytes)", target, len(body))
		return sess, fmt.Errorf("csrf token not found in body from %s", target)
	}
	sess.CSRF = string(m[1])
	logging.Logf("shelter: csrf token extracted from body ok")

	return sess, nil
}

type registerResponse struct {
	Ok bool `json:"ok"`
}

func registerIP(client *http.Client, sess sessionData, publicIP, dnsKey, registerURL string) (*http.Response, registerResponse, []byte, error) {
	form := url.Values{}
	form.Set("token", dnsKey)
	form.Set("ip_addr", publicIP)
	form.Set("skip_phone", "true")
	form.Set("_token", sess.CSRF)

	req, err := http.NewRequest(http.MethodPost, registerURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, registerResponse{}, nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "XSRF-TOKEN", Value: sess.XSRFToken})
	req.AddCookie(&http.Cookie{Name: "laravel_session", Value: sess.LaravelSession})

	logging.Logf("shelter: POST %s body: token=%s ip_addr=%s skip_phone=true _token=%s", registerURL, dnsKey, publicIP, short(sess.CSRF))
	resp, err := client.Do(req)
	if err != nil {
		logging.Logf("shelter: POST %s failed: %v", registerURL, err)
		return nil, registerResponse{}, nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logging.Logf("shelter: reading register-ip response body failed: %v", err)
		return resp, registerResponse{}, nil, err
	}

	logging.Logf("shelter: register-ip response — status %d body: %s", resp.StatusCode, string(body))

	var parsed registerResponse
	_ = json.Unmarshal(body, &parsed)
	logging.Logf("shelter: register-ip parsed ok=%v", parsed.Ok)

	return resp, parsed, body, nil
}

// tryDomain: one domain, full flow — fetch session, register ip.
// any step fail → caller moves to next domain.
func tryDomain(client *http.Client, domain, dnsKey, publicIP string) error {
	sessionURL := domain + "/ip/" + dnsKey
	registerURL := domain + "/register-ip"

	logging.Logf("shelter: trying domain %s", domain)

	sess, err := fetchOnePanel(client, sessionURL)
	if err != nil {
		return fmt.Errorf("fetch session: %w", err)
	}

	resp, parsed, body, err := registerIP(client, sess, publicIP, dnsKey, registerURL)
	if err != nil {
		return fmt.Errorf("register ip: %w", err)
	}
	if resp.StatusCode != 200 || !parsed.Ok {
		return fmt.Errorf("register-ip not ok: status %d body %s", resp.StatusCode, string(body))
	}
	return nil
}

type State string

const (
	Disconnected State = "disconnected"
	Connecting   State = "connecting"
	Connected    State = "connected"
	Failed       State = "failed"
)

type Status struct {
	State     State     `json:"state"`
	IP        string    `json:"ip"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Connect: try each domain in order, full flow per domain (session+register).
// first domain to fully succeed wins. then switch system dns, verify.
func Connect(publicIP, dnsKey, dns1, dns2 string) (Status, error) {
	logging.Logf("shelter: connect attempt starting — ip=%s dnskey=%s dns1=%s dns2=%s", publicIP, dnsKey, dns1, dns2)

	panelClient := dns.NewHTTPClient(dns.FallbackDNS1, 15*time.Second)

	var lastErr error
	ok := false
	for _, domain := range panelDomains {
		if err := tryDomain(panelClient, domain, dnsKey, publicIP); err != nil {
			logging.Logf("shelter: domain %s failed: %v", domain, err)
			lastErr = err
			continue
		}
		logging.Logf("shelter: domain %s succeeded", domain)
		ok = true
		break
	}
	if !ok {
		logging.Logf("shelter: connect FAILED — all domains exhausted")
		return failStatus(publicIP), fmt.Errorf("all panel domains failed: %w", lastErr)
	}

	if err := dns.SetSystemDNS(dns1, dns2); err != nil {
		logging.Logf("shelter: registered but set-dns FAILED: %v", err)
		return failStatus(publicIP), fmt.Errorf("registered but set dns failed: %w", err)
	}

	var verified bool
	var detail string
	for attempt := 1; attempt <= 3; attempt++ {
		verified, detail = dns.VerifyDNS(dns1, dns2)
		if verified {
			break
		}
		logging.Logf("shelter: dns verify attempt %d/3 failed: %s", attempt, detail)
		if attempt < 3 {
			time.Sleep(time.Duration(attempt) * time.Second)
		}
	}
	if !verified {
		logging.Logf("shelter: dns set but did not resolve after retries: %s", detail)
		return failStatus(publicIP), fmt.Errorf("dns set but did not resolve after retries: %s", detail)
	}

	logging.Logf("shelter: connect + dns fully verified: %s", detail)
	return Status{State: Connected, IP: publicIP, UpdatedAt: time.Now()}, nil
}

func failStatus(ip string) Status {
	return Status{State: Failed, IP: ip, UpdatedAt: time.Now()}
}
