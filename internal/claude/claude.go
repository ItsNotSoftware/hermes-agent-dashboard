// Package claude collects Claude usage from the Anthropic OAuth endpoint
// and falls back to scanning local .claude session logs. It mirrors
// fetch_claude_usage() in server.py.
package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	tokenURL = "https://platform.claude.com/v1/oauth/token"
	usageURL = "https://api.anthropic.com/api/oauth/usage"
	clientID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	scope    = "user:profile user:inference user:sessions:claude_code user:mcp_servers"
)

type Window struct {
	UsedPercent         *float64 `json:"used_percent"`
	LimitWindowSeconds  int64    `json:"limit_window_seconds"`
	ResetAfterSeconds   int64    `json:"reset_after_seconds"`
	ResetAt             *int64   `json:"reset_at"`
	InputTokens         int64    `json:"input_tokens,omitempty"`
	OutputTokens        int64    `json:"output_tokens,omitempty"`
	CacheReadTokens     int64    `json:"cache_read_tokens,omitempty"`
	CacheCreationTokens int64    `json:"cache_creation_tokens,omitempty"`
	WebSearchRequests   int64    `json:"web_search_requests,omitempty"`
	TotalTokens         int64    `json:"total_tokens,omitempty"`
	Messages            int64    `json:"messages,omitempty"`
	OldestTS            *float64 `json:"oldest_ts,omitempty"`
	NewestTS            *float64 `json:"newest_ts,omitempty"`
}

type ModelUsage struct {
	Model               string  `json:"model"`
	InputTokens         int64   `json:"input_tokens"`
	OutputTokens        int64   `json:"output_tokens"`
	CacheReadTokens     int64   `json:"cache_read_tokens"`
	CacheCreationTokens int64   `json:"cache_creation_tokens"`
	WebSearchRequests   int64   `json:"web_search_requests"`
	CostUSD             float64 `json:"cost_usd"`
}

type Totals struct {
	InputTokens         int64   `json:"input_tokens"`
	OutputTokens        int64   `json:"output_tokens"`
	CacheReadTokens     int64   `json:"cache_read_tokens"`
	CacheCreationTokens int64   `json:"cache_creation_tokens"`
	WebSearchRequests   int64   `json:"web_search_requests"`
	CostUSD             float64 `json:"cost_usd"`
}

type Usage struct {
	SubscriptionType         string         `json:"subscription_type"`
	UsageSource              string         `json:"usage_source"` // "api" | "local"
	FiveHourWindow           *Window        `json:"five_hour_window,omitempty"`
	OneWeekWindow            *Window        `json:"one_week_window,omitempty"`
	OpusWeekWindow           *Window        `json:"opus_week_window,omitempty"`
	OmeletteWeekWindow       *Window        `json:"omelette_week_window,omitempty"`
	ExtraUsage               map[string]any `json:"extra_usage,omitempty"`
	BillingType              string         `json:"billing_type,omitempty"`
	ExtraUsageEnabled        bool           `json:"extra_usage_enabled,omitempty"`
	ProjectPath              string         `json:"project_path,omitempty"`
	RecentSessionCostUSD     float64        `json:"recent_session_cost_usd,omitempty"`
	RecentSessionTotalTokens int64          `json:"recent_session_total_tokens,omitempty"`
	Models                   []ModelUsage   `json:"models,omitempty"`
	TotalsAcrossModels       Totals         `json:"totals"`
	RateLimitTier            string         `json:"rate_limit_tier,omitempty"`
}

func credentialsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", ".credentials.json")
}

func statePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude.json")
}

type oauthState struct {
	access    string
	refresh   string
	expiresAt int64 // millis
	subType   string
	tier      string
	root      map[string]any
}

func loadOAuth() (*oauthState, error) {
	raw, err := os.ReadFile(credentialsPath())
	if err != nil {
		return nil, err
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, err
	}
	s, _ := root["claudeAiOauth"].(map[string]any)
	if s == nil {
		return nil, fmt.Errorf("claude: claudeAiOauth missing")
	}
	access := strings.TrimSpace(stringOf(s["accessToken"]))
	refresh := strings.TrimSpace(stringOf(s["refreshToken"]))
	if access == "" || refresh == "" {
		return nil, fmt.Errorf("claude: tokens missing")
	}
	return &oauthState{
		access:    access,
		refresh:   refresh,
		expiresAt: toInt(s["expiresAt"]),
		subType:   strings.TrimSpace(stringOf(s["subscriptionType"])),
		tier:      strings.TrimSpace(stringOf(s["rateLimitTier"])),
		root:      root,
	}, nil
}

func saveOAuth(o *oauthState) error {
	s, _ := o.root["claudeAiOauth"].(map[string]any)
	if s == nil {
		s = map[string]any{}
		o.root["claudeAiOauth"] = s
	}
	s["accessToken"] = o.access
	s["refreshToken"] = o.refresh
	if o.expiresAt > 0 {
		s["expiresAt"] = o.expiresAt
	}
	out, err := json.MarshalIndent(o.root, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(credentialsPath(), out, 0o600); err != nil {
		return err
	}
	return os.Chmod(credentialsPath(), 0o600)
}

func (o *oauthState) refresh_(ctx context.Context) error {
	body, _ := json.Marshal(map[string]any{
		"grant_type":    "refresh_token",
		"refresh_token": o.refresh,
		"client_id":     clientID,
		"scope":         scope,
	})
	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("claude refresh HTTP %d: %s", resp.StatusCode, trunc(string(raw), 200))
	}
	var p struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return err
	}
	if strings.TrimSpace(p.AccessToken) == "" {
		return fmt.Errorf("claude refresh: no access_token")
	}
	o.access = strings.TrimSpace(p.AccessToken)
	if r := strings.TrimSpace(p.RefreshToken); r != "" {
		o.refresh = r
	}
	if p.ExpiresIn > 0 {
		o.expiresAt = time.Now().UnixMilli() + p.ExpiresIn*1000
	}
	return saveOAuth(o)
}

func (o *oauthState) expiringSoon() bool {
	if o.expiresAt == 0 {
		return false
	}
	return time.Now().UnixMilli()+5*60*1000 >= o.expiresAt
}

// Fetch returns Claude usage, preferring the API and falling back to local jsonl scan.
func Fetch(ctx context.Context, dashboardDir string) (*Usage, error) {
	// Locate the right project bucket and tally per-model usage from ~/.claude.json
	projectPath, models, totals, recentCost := projectFromState(dashboardDir)

	usage := &Usage{
		ProjectPath:              projectPath,
		Models:                   models,
		TotalsAcrossModels:       totals,
		RecentSessionCostUSD:     recentCost,
		RecentSessionTotalTokens: totals.InputTokens + totals.OutputTokens + totals.CacheReadTokens + totals.CacheCreationTokens,
	}

	if o, err := loadOAuth(); err == nil {
		usage.SubscriptionType = o.subType
		if usage.SubscriptionType == "" {
			usage.SubscriptionType = "unknown"
		}
		usage.RateLimitTier = o.tier

		if o.expiringSoon() {
			_ = o.refresh_(ctx) // best-effort
		}
		body, status, err := requestUsage(ctx, o.access)
		if (status == 401 || status == 403) && err != nil {
			if rErr := o.refresh_(ctx); rErr == nil {
				body, status, err = requestUsage(ctx, o.access)
			}
		}
		if err == nil && status == 200 {
			now := float64(time.Now().Unix())
			applyAPIWindows(usage, body, now)
			usage.UsageSource = "api"
			return usage, nil
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "claude:", err)
		}
	}

	// Local fallback: scan session jsonl files to derive 5h/1w totals.
	now := time.Now().Unix()
	five := scanProjectUsage(projectPath, now-5*60*60)
	week := scanProjectUsage(projectPath, now-7*24*60*60)

	usage.UsageSource = "local"
	usage.FiveHourWindow = &Window{
		LimitWindowSeconds: 5 * 60 * 60,
		ResetAfterSeconds:  resetLeft(five.oldestTS, 5*60*60, now),
		InputTokens:        five.input, OutputTokens: five.output,
		CacheReadTokens: five.cacheRead, CacheCreationTokens: five.cacheCreation,
		WebSearchRequests: five.webSearch, TotalTokens: five.total, Messages: five.messages,
		OldestTS: nullableTS(five.oldestTS), NewestTS: nullableTS(five.newestTS),
	}
	usage.OneWeekWindow = &Window{
		LimitWindowSeconds: 7 * 24 * 60 * 60,
		ResetAfterSeconds:  resetLeft(week.oldestTS, 7*24*60*60, now),
		InputTokens:        week.input, OutputTokens: week.output,
		CacheReadTokens: week.cacheRead, CacheCreationTokens: week.cacheCreation,
		WebSearchRequests: week.webSearch, TotalTokens: week.total, Messages: week.messages,
		OldestTS: nullableTS(week.oldestTS), NewestTS: nullableTS(week.newestTS),
	}
	return usage, nil
}

func requestUsage(ctx context.Context, accessToken string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", usageURL, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")

	cli := &http.Client{Timeout: 30 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return body, resp.StatusCode, fmt.Errorf("HTTP %d: %s", resp.StatusCode, trunc(string(body), 200))
	}
	return body, resp.StatusCode, nil
}

func applyAPIWindows(u *Usage, body []byte, nowTS float64) {
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return
	}
	if s, ok := data["subscription_type"].(string); ok && strings.TrimSpace(s) != "" {
		u.SubscriptionType = s
	}
	u.FiveHourWindow = apiWindow(data["five_hour"], 5*60*60, nowTS)
	u.OneWeekWindow = apiWindow(data["seven_day"], 7*24*60*60, nowTS)
	if opus, ok := data["seven_day_opus"].(map[string]any); ok && len(opus) > 0 {
		u.OpusWeekWindow = apiWindow(opus, 7*24*60*60, nowTS)
	}
	if om, ok := data["seven_day_omelette"].(map[string]any); ok && len(om) > 0 {
		u.OmeletteWeekWindow = apiWindow(om, 7*24*60*60, nowTS)
	}
	if eu, ok := data["extra_usage"].(map[string]any); ok {
		u.ExtraUsage = eu
	}
}

func apiWindow(src any, windowSeconds int64, nowTS float64) *Window {
	m, ok := src.(map[string]any)
	if !ok {
		return &Window{LimitWindowSeconds: windowSeconds}
	}
	used := toFloat(m["utilization"])
	w := &Window{
		UsedPercent:        &used,
		LimitWindowSeconds: windowSeconds,
		ResetAfterSeconds:  resetAfter(m["resets_at"], nowTS),
	}
	if t := parseTSFromAny(m["resets_at"]); t > 0 {
		w.ResetAt = ptrInt(int64(t))
	}
	return w
}

func resetAfter(v any, nowTS float64) int64 {
	switch x := v.(type) {
	case float64:
		t := x
		if t > 1e10 {
			t /= 1000
		}
		if t-nowTS < 0 {
			return 0
		}
		return int64(t - nowTS)
	case string:
		t := parseTSFromAny(x)
		if t-nowTS < 0 {
			return 0
		}
		return int64(t - nowTS)
	}
	return 0
}

func parseTSFromAny(v any) float64 {
	switch x := v.(type) {
	case string:
		if x == "" {
			return 0
		}
		x = strings.Replace(x, "Z", "+00:00", 1)
		t, err := time.Parse(time.RFC3339Nano, x)
		if err != nil {
			t, err = time.Parse(time.RFC3339, x)
			if err != nil {
				return 0
			}
		}
		return float64(t.Unix())
	case float64:
		if x > 1e10 {
			return x / 1000
		}
		return x
	}
	return 0
}

// ---------- project state ----------

func projectFromState(dashboardDir string) (string, []ModelUsage, Totals, float64) {
	raw, err := os.ReadFile(statePath())
	if err != nil {
		return "", nil, Totals{}, 0
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return "", nil, Totals{}, 0
	}
	projects, _ := root["projects"].(map[string]any)
	if projects == nil {
		return "", nil, Totals{}, 0
	}
	home, _ := os.UserHomeDir()
	preferred := []string{dashboardDir, filepath.Join(home, "dashboard"), home}
	projectPath := ""
	for _, p := range preferred {
		if _, ok := projects[p]; ok {
			projectPath = p
			break
		}
	}
	if projectPath == "" {
		for path, info := range projects {
			m, _ := info.(map[string]any)
			if m == nil {
				continue
			}
			if _, ok := m["lastModelUsage"]; ok {
				projectPath = path
				break
			}
			if _, ok := m["lastCost"]; ok {
				projectPath = path
				break
			}
		}
	}
	if projectPath == "" {
		return "", nil, Totals{}, 0
	}

	project, _ := projects[projectPath].(map[string]any)
	rawModels, _ := project["lastModelUsage"].(map[string]any)
	models := make([]ModelUsage, 0, len(rawModels))
	var totals Totals

	for name, info := range rawModels {
		m, _ := info.(map[string]any)
		if m == nil {
			continue
		}
		mu := ModelUsage{
			Model:               name,
			InputTokens:         toInt(m["inputTokens"]),
			OutputTokens:        toInt(m["outputTokens"]),
			CacheReadTokens:     toInt(m["cacheReadInputTokens"]),
			CacheCreationTokens: toInt(m["cacheCreationInputTokens"]),
			WebSearchRequests:   toInt(m["webSearchRequests"]),
			CostUSD:             toFloat(m["costUSD"]),
		}
		totals.InputTokens += mu.InputTokens
		totals.OutputTokens += mu.OutputTokens
		totals.CacheReadTokens += mu.CacheReadTokens
		totals.CacheCreationTokens += mu.CacheCreationTokens
		totals.WebSearchRequests += mu.WebSearchRequests
		totals.CostUSD += mu.CostUSD
		models = append(models, mu)
	}
	sort.Slice(models, func(i, j int) bool { return models[i].CostUSD > models[j].CostUSD })

	recentCost := totals.CostUSD
	if v, ok := project["lastCost"]; ok {
		recentCost = toFloat(v)
	}
	return projectPath, models, totals, recentCost
}

// ---------- local jsonl fallback ----------

type scanTotals struct {
	input, output, cacheRead, cacheCreation, webSearch, total, messages int64
	oldestTS, newestTS                                                  float64
}

func projectDir(projectPath string) string {
	if projectPath == "" {
		return ""
	}
	slug := strings.ReplaceAll(projectPath, "/", "-")
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "projects", slug)
}

func scanProjectUsage(projectPath string, sinceTS int64) scanTotals {
	dir := projectDir(projectPath)
	if dir == "" {
		return scanTotals{}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return scanTotals{}
	}
	var st scanTotals
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		f, err := os.Open(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for sc.Scan() {
			var entry map[string]any
			if err := json.Unmarshal(sc.Bytes(), &entry); err != nil {
				continue
			}
			ts := parseTSFromAny(entry["timestamp"])
			if ts == 0 || int64(ts) < sinceTS {
				continue
			}
			msg, _ := entry["message"].(map[string]any)
			usage, _ := msg["usage"].(map[string]any)
			if usage == nil {
				continue
			}
			in := toInt(usage["input_tokens"])
			out := toInt(usage["output_tokens"])
			cr := toInt(usage["cache_read_input_tokens"])
			cc := toInt(usage["cache_creation_input_tokens"])
			ws := int64(0)
			if stu, ok := usage["server_tool_use"].(map[string]any); ok {
				ws = toInt(stu["web_search_requests"])
			}
			st.input += in
			st.output += out
			st.cacheRead += cr
			st.cacheCreation += cc
			st.webSearch += ws
			st.total += in + out + cr + cc
			st.messages++
			if st.oldestTS == 0 || ts < st.oldestTS {
				st.oldestTS = ts
			}
			if ts > st.newestTS {
				st.newestTS = ts
			}
		}
		f.Close()
	}
	return st
}

func resetLeft(oldestTS float64, windowSeconds, nowTS int64) int64 {
	if oldestTS == 0 {
		return windowSeconds
	}
	left := windowSeconds - (nowTS - int64(oldestTS))
	if left < 0 {
		return 0
	}
	return left
}

func nullableTS(v float64) *float64 {
	if v == 0 {
		return nil
	}
	return &v
}

// ---------- conversions ----------

func toFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	}
	return 0
}
func toInt(v any) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case int:
		return int64(x)
	case int64:
		return x
	}
	return 0
}
func stringOf(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
func ptrInt(v int64) *int64 { return &v }
