// Package openai fetches the OpenAI Codex plan usage that the Hermes auth
// file already has tokens for. It mirrors fetch_openai_plan_usage()
// in server.py: read tokens, refresh if expiring, GET /wham/usage,
// normalize the response.
package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/diogo/hermes-agent-dashboard/internal/util"
)

const (
	codexClientID = "app_EMoamEEZ73f0CkXaXp7hrann"
	tokenURL      = "https://auth.openai.com/oauth/token"
	usageURL      = "https://chatgpt.com/backend-api/wham/usage"
)

type Window struct {
	UsedPercent        float64 `json:"used_percent"`
	LimitWindowSeconds int64   `json:"limit_window_seconds"`
	ResetAfterSeconds  int64   `json:"reset_after_seconds"`
	ResetAt            int64   `json:"reset_at"`
}

type Plan struct {
	PlanType            string         `json:"plan_type"`
	Allowed             bool           `json:"allowed"`
	LimitReached        bool           `json:"limit_reached"`
	PrimaryWindow       Window         `json:"primary_window"`
	SecondaryWindow     Window         `json:"secondary_window"`
	CodeReviewRateLimit any            `json:"code_review_rate_limit,omitempty"`
	Credits             map[string]any `json:"credits,omitempty"`
}

func authPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".hermes", "auth.json")
}

type tokens struct {
	access  string
	refresh string
	root    map[string]any // entire auth.json, preserved on save
}

func loadTokens() (*tokens, error) {
	raw, err := os.ReadFile(authPath())
	if err != nil {
		return nil, err
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, err
	}
	providers, _ := root["providers"].(map[string]any)
	state, _ := providers["openai-codex"].(map[string]any)
	tk, _ := state["tokens"].(map[string]any)
	access, _ := tk["access_token"].(string)
	refresh, _ := tk["refresh_token"].(string)
	access = strings.TrimSpace(access)
	refresh = strings.TrimSpace(refresh)
	if access == "" || refresh == "" {
		return nil, fmt.Errorf("openai: tokens missing in %s", authPath())
	}
	return &tokens{access: access, refresh: refresh, root: root}, nil
}

func saveTokens(t *tokens) error {
	providers, _ := t.root["providers"].(map[string]any)
	if providers == nil {
		providers = map[string]any{}
		t.root["providers"] = providers
	}
	state, _ := providers["openai-codex"].(map[string]any)
	if state == nil {
		state = map[string]any{}
		providers["openai-codex"] = state
	}
	tk, _ := state["tokens"].(map[string]any)
	if tk == nil {
		tk = map[string]any{}
		state["tokens"] = tk
	}
	tk["access_token"] = t.access
	tk["refresh_token"] = t.refresh
	state["last_refresh"] = time.Now().UTC().Format("2006-01-02T15:04:05") + "Z"

	out, err := json.MarshalIndent(t.root, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(authPath(), out, 0o600); err != nil {
		return err
	}
	return os.Chmod(authPath(), 0o600)
}

func tokenExpiring(access string, skew time.Duration) bool {
	claims, err := util.DecodeJWTClaims(access)
	if err != nil {
		return false
	}
	exp, ok := claims["exp"].(float64)
	if !ok {
		return false
	}
	return time.Now().Add(skew).After(time.Unix(int64(exp), 0))
}

func refresh(ctx context.Context, t *tokens) error {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", t.refresh)
	form.Set("client_id", codexClientID)

	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("openai refresh HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return err
	}
	access := strings.TrimSpace(payload.AccessToken)
	if access == "" {
		return fmt.Errorf("openai refresh: no access_token in response")
	}
	t.access = access
	if r := strings.TrimSpace(payload.RefreshToken); r != "" {
		t.refresh = r
	}
	return saveTokens(t)
}

// Fetch returns the current plan usage, refreshing the token if needed.
// Returns nil if there are no tokens on disk.
func Fetch(ctx context.Context) (*Plan, error) {
	t, err := loadTokens()
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if tokenExpiring(t.access, 2*time.Minute) {
		if err := refresh(ctx, t); err != nil {
			// Continue with stale token; the API call may still succeed.
			fmt.Fprintln(os.Stderr, "openai:", err)
		}
	}

	plan, status, err := requestUsage(ctx, t.access)
	if (status == 401 || status == 403) && err != nil {
		if rErr := refresh(ctx, t); rErr == nil {
			plan, status, err = requestUsage(ctx, t.access)
		}
	}
	if err != nil {
		return nil, err
	}
	if status >= 400 {
		return nil, fmt.Errorf("openai usage HTTP %d", status)
	}
	return plan, nil
}

func requestUsage(ctx context.Context, accessToken string) (*Plan, int, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", usageURL, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	cli := &http.Client{Timeout: 10 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, resp.StatusCode, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	return normalize(body), resp.StatusCode, nil
}

func normalize(body []byte) *Plan {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil
	}
	rl, _ := raw["rate_limit"].(map[string]any)
	if rl == nil {
		rl = map[string]any{}
	}
	primary, _ := rl["primary_window"].(map[string]any)
	secondary, _ := rl["secondary_window"].(map[string]any)

	planType := "unknown"
	if v, ok := raw["plan_type"].(string); ok && strings.TrimSpace(v) != "" {
		planType = strings.TrimSpace(v)
	}
	credits, _ := raw["credits"].(map[string]any)

	plan := &Plan{
		PlanType:            planType,
		Allowed:             toBool(rl["allowed"]),
		LimitReached:        toBool(rl["limit_reached"]),
		PrimaryWindow:       window(primary),
		SecondaryWindow:     window(secondary),
		CodeReviewRateLimit: raw["code_review_rate_limit"],
		Credits:             credits,
	}
	return plan
}

func window(m map[string]any) Window {
	if m == nil {
		return Window{}
	}
	return Window{
		UsedPercent:        toFloat(m["used_percent"]),
		LimitWindowSeconds: toInt(m["limit_window_seconds"]),
		ResetAfterSeconds:  toInt(m["reset_after_seconds"]),
		ResetAt:            toInt(m["reset_at"]),
	}
}

func toBool(v any) bool { b, _ := v.(bool); return b }
func toFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case json.Number:
		f, _ := x.Float64()
		return f
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
	case json.Number:
		n, _ := x.Int64()
		return n
	case int:
		return int64(x)
	case int64:
		return x
	}
	return 0
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
