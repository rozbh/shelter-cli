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

const (
	panelBase1  = "https://panel3.sheltertm.com/ip"
	panelBase2  = "https://panel2.sheltertm.com/ip"
	registerURL = "https://panel3.sheltertm.com/register-ip"

	fallbackDNS1 = "8.8.8.8"
	fallbackDNS2 = "1.1.1.1"
)

var csrfRe = regexp.MustCompile(`const\s+csrf\s*=\s*"([^"]+)"`)

type sessionData struct {
	XSRFToken      string
	LaravelSession string
	CSRF           string
}

// fetchPanelSession tries panel3 first, falls back to panel2. client
// resolves hostnames against a known-good dns (caller passes fallback dns,
// not system dns — see Connect).
func fetchPanelSession(client *http.Client, dnsKey string) (sessionData, error) {
	url1 := panelBase1 + "/" + dnsKey
	url2 := panelBase2 + "/" + dnsKey

	logging.Logf("shelter: trying panel3 -> %s", url1)
	sess, err := fetchOnePanel(client, url1)
	if err == nil {
		logging.Logf("shelter: panel3 ok — xsrf=%s session=%s csrf=%s", short(sess.XSRFToken), short(sess.LaravelSession), short(sess.CSRF))
		return sess, nil
	}
	logging.Logf("shelter: panel3 failed: %v — falling back to panel2 -> %s", err, url2)

	sess2, err2 := fetchOnePanel(client, url2)
	if err2 != nil {
		logging.Logf("shelter: panel2 also failed: %v", err2)
		return sessionData{}, fmt.Errorf("both panels failed: panel3: %v | panel2: %v", err, err2)
	}
	logging.Logf("shelter: panel2 ok — xsrf=%s session=%s csrf=%s", short(sess2.XSRFToken), short(sess2.LaravelSession), short(sess2.CSRF))
	return sess2, nil
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

func registerIP(client *http.Client, sess sessionData, publicIP, dnsKey string) (*http.Response, registerResponse, []byte, error) {
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

// Connect: fetch panel session -> register ip -> switch system dns to
// dns1/dns2 -> verify it resolves. all panel/register http traffic goes
// through a client bound to fallbackDNS1, independent of whatever the
// system resolver is doing — same guarantee on linux/mac/windows since it's
// plain Go networking, not an OS command.
func Connect(publicIP, dnsKey, dns1, dns2 string) (Status, error) {
	logging.Logf("shelter: connect attempt starting — ip=%s dnskey=%s dns1=%s dns2=%s", publicIP, dnsKey, dns1, dns2)

	panelClient := dns.NewHTTPClient(dns.FallbackDNS1, 15*time.Second)

	sess, err := fetchPanelSession(panelClient, dnsKey)
	if err != nil {
		logging.Logf("shelter: connect FAILED at fetch-session stage: %v", err)
		return failStatus(publicIP), fmt.Errorf("fetch session: %w", err)
	}

	resp, parsed, body, err := registerIP(panelClient, sess, publicIP, dnsKey)
	if err != nil {
		logging.Logf("shelter: connect FAILED at register-ip stage: %v", err)
		return failStatus(publicIP), fmt.Errorf("register ip: %w", err)
	}

	if resp.StatusCode != 200 || !parsed.Ok {
		logging.Logf("shelter: connect FAILED — register-ip not ok (status=%d ok=%v)", resp.StatusCode, parsed.Ok)
		return failStatus(publicIP), fmt.Errorf("register-ip not ok: status %d body %s", resp.StatusCode, string(body))
	}

	if err := dns.SetSystemDNS(dns1, dns2); err != nil {
		logging.Logf("shelter: registered but set-dns FAILED: %v", err)
		return failStatus(publicIP), fmt.Errorf("registered but set dns failed: %w", err)
	}

	// nmcli/resolvectl reactivate = brief iface flap right after set.
	// first verify can hit that dead window → retry w/ backoff before fail.
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

// internal/shelter/shelter.go
func failStatus(ip string) Status {
	return Status{State: Failed, IP: ip, UpdatedAt: time.Now()}
}
