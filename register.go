package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	panelBase1  = "https://panel3.sheltertm.com/ip"
	panelBase2  = "https://panel2.sheltertm.com/ip"
	registerURL = "https://panel3.sheltertm.com/register-ip"
)

var csrfRe = regexp.MustCompile(`const\s+csrf\s*=\s*"([^"]+)"`)

// sessionData holds what we pull from the panel page: cookies + csrf.
type sessionData struct {
	XSRFToken      string
	LaravelSession string
	CSRF           string
}

// fetchPanelSession tries panel3 first, falls back to panel2.
// urls are built as <base>/<dnskey> — dnskey comes from the user's config.
// grabs XSRF-TOKEN + laravel_session cookies from Set-Cookie headers,
// and pulls csrf value out of page body.
func fetchPanelSession(dnsKey string) (sessionData, error) {
	url1 := panelBase1 + "/" + dnsKey
	url2 := panelBase2 + "/" + dnsKey

	logf("shelter: trying panel3 -> %s", url1)
	sess, err := fetchOnePanel(url1)
	if err == nil {
		logf("shelter: panel3 ok — xsrf=%s session=%s csrf=%s", short(sess.XSRFToken), short(sess.LaravelSession), short(sess.CSRF))
		return sess, nil
	}
	logf("shelter: panel3 failed: %v — falling back to panel2 -> %s", err, url2)

	sess2, err2 := fetchOnePanel(url2)
	if err2 != nil {
		logf("shelter: panel2 also failed: %v", err2)
		return sessionData{}, fmt.Errorf("both panels failed: panel3: %v | panel2: %v", err, err2)
	}
	logf("shelter: panel2 ok — xsrf=%s session=%s csrf=%s", short(sess2.XSRFToken), short(sess2.LaravelSession), short(sess2.CSRF))
	return sess2, nil
}

// short truncates a token for logging so we don't dump full secrets to stderr.
func short(s string) string {
	if len(s) <= 10 {
		return s
	}
	return s[:10] + "..."
}

func fetchOnePanel(target string) (sessionData, error) {
	var sess sessionData

	client := &http.Client{
		// don't follow redirects automatically so we don't lose Set-Cookie headers
		// from the first hop if the panel ever redirects; adjust if needed.
	}

	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return sess, err
	}

	resp, err := client.Do(req)
	if err != nil {
		logf("shelter: GET %s failed: %v", target, err)
		return sess, err
	}
	defer resp.Body.Close()

	logf("shelter: GET %s -> status %d", target, resp.StatusCode)

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
	logf("shelter: cookies found — XSRF-TOKEN present=%v laravel_session present=%v", sess.XSRFToken != "", sess.LaravelSession != "")
	if sess.XSRFToken == "" || sess.LaravelSession == "" {
		return sess, fmt.Errorf("missing cookies from %s (xsrf=%q session=%q)", target, sess.XSRFToken, sess.LaravelSession)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return sess, err
	}

	m := csrfRe.FindSubmatch(body)
	if len(m) != 2 {
		logf("shelter: csrf regex did not match body from %s (body length %d bytes)", target, len(body))
		return sess, fmt.Errorf("csrf token not found in body from %s", target)
	}
	sess.CSRF = string(m[1])
	logf("shelter: csrf token extracted from body ok")

	return sess, nil
}

// registerResponse is the expected JSON body from /register-ip.
type registerResponse struct {
	Ok bool `json:"ok"`
}

// registerIP posts the ip to /register-ip using the cookies + csrf we captured.
func registerIP(sess sessionData, publicIP, dnsKey string) (*http.Response, registerResponse, []byte, error) {
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

	// re-attach the cookies we pulled off the GET request
	req.AddCookie(&http.Cookie{Name: "XSRF-TOKEN", Value: sess.XSRFToken})
	req.AddCookie(&http.Cookie{Name: "laravel_session", Value: sess.LaravelSession})

	client := &http.Client{}
	logf("shelter: POST %s body: token=%s ip_addr=%s skip_phone=true _token=%s", registerURL, dnsKey, publicIP, short(sess.CSRF))
	resp, err := client.Do(req)
	if err != nil {
		logf("shelter: POST %s failed: %v", registerURL, err)
		return nil, registerResponse{}, nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logf("shelter: reading register-ip response body failed: %v", err)
		return resp, registerResponse{}, nil, err
	}

	logf("shelter: register-ip response — status %d body: %s", resp.StatusCode, string(body))

	var parsed registerResponse
	_ = json.Unmarshal(body, &parsed) // if body isn't JSON, parsed.Ok just stays false
	logf("shelter: register-ip parsed ok=%v", parsed.Ok)

	return resp, parsed, body, nil
}

// ---- shelter-status: persisted "are we connected to shelter" state ----

type shelterState string

const (
	shelterDisconnected shelterState = "disconnected" // no internet, or never tried
	shelterConnecting   shelterState = "connecting"   // request in flight
	shelterConnected    shelterState = "connected"    // register-ip succeeded
	shelterFailed       shelterState = "failed"       // internet up but register-ip failed
)

// shelterStatus holds the current in-memory shelter connection state for
// this run only. Intentionally NOT persisted to disk — a status from a
// previous run doesn't tell us anything true about right now, so every
// app start begins at shelterDisconnected and re-earns its status live.
type shelterStatus struct {
	Status       shelterState `json:"status"`
	IP           string       `json:"ip"`
	UpdatedAt    time.Time    `json:"updated_at"`
	DNSVerified  bool         `json:"dns_verified"`
	DNSVerifyMsg string       `json:"dns_verify_msg"`
}

// connectShelter does the full flow: fetch panel session -> register ip ->
// on 200 + ok:true, apply dns1/dns2 as the system DNS, then verify it resolves.
// dnsKey is the user's dnskey from config — used both for the panel url and
// the register-ip "token" field.
func connectShelter(publicIP, dnsKey, dns1, dns2 string) (shelterStatus, error) {
	logf("shelter: connect attempt starting — ip=%s dnskey=%s dns1=%s dns2=%s", publicIP, dnsKey, dns1, dns2)

	sess, err := fetchPanelSession(dnsKey)
	if err != nil {
		logf("shelter: connect FAILED at fetch-session stage: %v", err)
		st := shelterStatus{Status: shelterFailed, IP: publicIP, UpdatedAt: time.Now()}
		return st, fmt.Errorf("fetch session: %w", err)
	}

	resp, parsed, body, err := registerIP(sess, publicIP, dnsKey)
	if err != nil {
		logf("shelter: connect FAILED at register-ip stage: %v", err)
		st := shelterStatus{Status: shelterFailed, IP: publicIP, UpdatedAt: time.Now()}
		return st, fmt.Errorf("register ip: %w", err)
	}

	if resp.StatusCode != 200 || !parsed.Ok {
		logf("shelter: connect FAILED — register-ip not ok (status=%d ok=%v)", resp.StatusCode, parsed.Ok)
		st := shelterStatus{Status: shelterFailed, IP: publicIP, UpdatedAt: time.Now()}
		return st, fmt.Errorf("register-ip not ok: status %d body %s", resp.StatusCode, string(body))
	}
	logf("shelter: register-ip confirmed ok — proceeding to set dns")

	// success: status 200 + ok:true -> point system DNS at dns1/dns2
	dnsErr := setSystemDNS(dns1, dns2)

	st := shelterStatus{Status: shelterConnected, IP: publicIP, UpdatedAt: time.Now()}

	if dnsErr != nil {
		logf("shelter: connect OK but set-dns FAILED: %v", dnsErr)
		return st, fmt.Errorf("connected but set dns failed: %w", dnsErr)
	}
	logf("shelter: set-dns command completed, verifying resolution now")

	// dns command "succeeded" — now actually prove it resolves through the
	// new servers before calling the job done.
	verified, detail := verifyDNS(dns1, dns2)
	st.DNSVerified = verified
	st.DNSVerifyMsg = detail

	if !verified {
		logf("shelter: connect OK, dns set, but VERIFY FAILED: %s", detail)
		return st, fmt.Errorf("dns set but verification failed: %s", detail)
	}
	logf("shelter: connect + dns fully verified: %s", detail)
	return st, nil
}
