package tui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"time"

	"shelter-cli/internal/config"
)

// fixed check targets — always the same, user never edits these.
const (
	fixedCheck1 = "8.8.8.8"
	fixedCheck2 = "1.1.1.1"
)

// checkResult is one row in the connectivity table.
type checkResult struct {
	Label   string // e.g. "Public IP", "Internet", "DNS/Internet"
	Target  string // e.g. "8.8.8.8", "1.1.1.1", "example.com"
	OK      bool
	Latency string // ping round-trip time in ms ("timeout" if no reply)
}

var rttRe = regexp.MustCompile(`time[=<]([0-9.]+)\s*ms`)

// pingTarget runs one ICMP ping and returns ok + round-trip latency string.
func pingTarget(host string) (bool, string) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("ping", "-n", "1", "-w", "10000", host)
	} else {
		cmd = exec.Command("ping", "-c", "1", "-W", "10", host)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, "timeout"
	}
	m := rttRe.FindStringSubmatch(string(out))
	if len(m) == 2 {
		if v, perr := strconv.ParseFloat(m[1], 64); perr == nil {
			return true, fmt.Sprintf("%.0fms", v)
		}
		return true, m[1] + "ms"
	}
	return false, "timeout"
}

// getPublicIP fetches public IP (no ping — plain HTTP lookup).
func getPublicIP() (string, error) {
	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://api.ipify.org?format=json")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var data struct {
		IP string `json:"ip"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return "", err
	}
	return data.IP, nil
}

// runChecks fetches public IP + runs all pings concurrently, returns table rows in order.
func runChecks(cfg config.Config) []checkResult {
	type job struct {
		label, target string
	}
	jobs := []job{
		{"Internet", fixedCheck1},
		{"Internet", fixedCheck2},
		{"DNS/Internet", "google.com"},
		{"DNS/Intranet", "soft98.ir"},
	}

	pingResults := make([]checkResult, len(jobs))
	done := make(chan struct{}, len(jobs))
	for i, j := range jobs {
		go func(i int, label, target string) {
			ok, lat := pingTarget(target)
			pingResults[i] = checkResult{Label: label, Target: target, OK: ok, Latency: lat}
			done <- struct{}{}
		}(i, j.label, j.target)
	}

	var ipRow checkResult
	ipDone := make(chan struct{})
	go func() {
		ip, err := getPublicIP()
		if err != nil {
			ipRow = checkResult{Label: "Public IP", Target: "N/A", OK: false, Latency: "-"}
		} else {
			ipRow = checkResult{Label: "Public IP", Target: ip, OK: true, Latency: "-"}
		}
		close(ipDone)
	}()

	for range jobs {
		<-done
	}
	<-ipDone

	return append([]checkResult{ipRow}, pingResults...)
}

// internetUp reports true if either fixed ping target answered.
func internetUp(checks []checkResult) bool {
	for _, c := range checks {
		if c.Label == "Internet" && c.OK {
			return true
		}
	}
	return false
}

// publicIPFrom pulls the "Public IP" row value out of checks, "" if not found/failed.
func publicIPFrom(checks []checkResult) string {
	for _, c := range checks {
		if c.Label == "Public IP" && c.OK {
			return c.Target
		}
	}
	return ""
}
