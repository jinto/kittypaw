package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Client communicates with a GoPaw server instance via REST API.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// New creates a new Client targeting the given server address.
func New(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		http: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// Health checks daemon liveness. Returns nil if healthy.
func (c *Client) Health() error {
	_, err := c.get("/health")
	return err
}

// Status returns today's execution statistics.
func (c *Client) Status() (map[string]any, error) {
	return c.get("/api/v1/status")
}

// Executions returns recent execution records, optionally filtered by skill name.
func (c *Client) Executions(skill string, limit int) (map[string]any, error) {
	path := fmt.Sprintf("/api/v1/executions?limit=%d", limit)
	if skill != "" {
		path += "&skill=" + url.QueryEscape(skill)
	}
	return c.get(path)
}

// Agents returns configured agents.
func (c *Client) Agents() (map[string]any, error) {
	return c.get("/api/v1/agents")
}

// Skills returns all skills.
func (c *Client) Skills() (map[string]any, error) {
	return c.get("/api/v1/skills")
}

// RunSkill dispatches a skill by name.
func (c *Client) RunSkill(name string) (map[string]any, error) {
	return c.post("/api/v1/skills/run", map[string]string{"name": name})
}

// Teach creates a skill from a description.
func (c *Client) Teach(description string) (map[string]any, error) {
	return c.post("/api/v1/skills/teach", map[string]string{"description": description})
}

// Chat sends a chat message and returns the response.
func (c *Client) Chat(text, sessionID string) (map[string]any, error) {
	body := map[string]string{"text": text}
	if sessionID != "" {
		body["session_id"] = sessionID
	}
	return c.post("/api/v1/chat", body)
}

// DeleteSkill removes a skill by name.
func (c *Client) DeleteSkill(name string) (map[string]any, error) {
	return c.delete("/api/v1/skills/" + url.PathEscape(name))
}

// DisableSkill disables a skill by name.
func (c *Client) DisableSkill(name string) (map[string]any, error) {
	return c.post("/api/v1/skills/"+url.PathEscape(name)+"/disable", nil)
}

// ConfigCheck returns configuration summary.
func (c *Client) ConfigCheck() (map[string]any, error) {
	return c.get("/api/v1/config/check")
}

// MemorySearch performs full-text search over execution history.
func (c *Client) MemorySearch(query string, limit int) (map[string]any, error) {
	return c.get(fmt.Sprintf("/api/v1/memory/search?q=%s&limit=%d", url.QueryEscape(query), limit))
}

// LinkIdentity links a channel user to a global identity.
func (c *Client) LinkIdentity(globalUserID, channel, channelUserID string) (map[string]any, error) {
	return c.post("/api/v1/users/link", map[string]string{
		"global_user_id":  globalUserID,
		"channel":         channel,
		"channel_user_id": channelUserID,
	})
}

// Reload triggers a config reload on the server.
func (c *Client) Reload() (map[string]any, error) {
	return c.post("/api/v1/reload", nil)
}

// EnableSkill sets a skill's enabled state to true.
func (c *Client) EnableSkill(name string) (map[string]any, error) {
	return c.post("/api/v1/skills/"+url.PathEscape(name)+"/enable", nil)
}

// ExplainSkill asks the LLM to explain a skill.
func (c *Client) ExplainSkill(name string) (map[string]any, error) {
	return c.post("/api/v1/skills/"+url.PathEscape(name)+"/explain", nil)
}

// SuggestionsList returns all pending suggestions.
func (c *Client) SuggestionsList() (map[string]any, error) {
	return c.get("/api/v1/suggestions")
}

// SuggestionsAccept accepts a suggestion by skill ID.
func (c *Client) SuggestionsAccept(skillID string) (map[string]any, error) {
	return c.post("/api/v1/suggestions/"+url.PathEscape(skillID)+"/accept", nil)
}

// SuggestionsDismiss dismisses a suggestion by skill ID.
func (c *Client) SuggestionsDismiss(skillID string) (map[string]any, error) {
	return c.post("/api/v1/suggestions/"+url.PathEscape(skillID)+"/dismiss", nil)
}

// FixesList returns fixes for a skill.
func (c *Client) FixesList(skillID string) (map[string]any, error) {
	return c.get("/api/v1/skills/" + url.PathEscape(skillID) + "/fixes")
}

// FixesApprove approves a fix by ID.
func (c *Client) FixesApprove(id string) (map[string]any, error) {
	return c.post("/api/v1/fixes/"+url.PathEscape(id)+"/approve", nil)
}

// ReflectionList returns reflection candidates.
func (c *Client) ReflectionList() (map[string]any, error) {
	return c.get("/api/v1/reflection")
}

// ReflectionApprove approves a reflection candidate.
func (c *Client) ReflectionApprove(key string) (map[string]any, error) {
	return c.post("/api/v1/reflection/"+url.PathEscape(key)+"/approve", nil)
}

// ReflectionReject rejects a reflection candidate.
func (c *Client) ReflectionReject(key string) (map[string]any, error) {
	return c.post("/api/v1/reflection/"+url.PathEscape(key)+"/reject", nil)
}

// ReflectionClear clears all reflection candidates.
func (c *Client) ReflectionClear() (map[string]any, error) {
	return c.post("/api/v1/reflection/clear", nil)
}

// ReflectionRun triggers a reflection cycle.
func (c *Client) ReflectionRun() (map[string]any, error) {
	return c.post("/api/v1/reflection/run", nil)
}

// WeeklyReport returns the weekly reflection report.
func (c *Client) WeeklyReport() (map[string]any, error) {
	return c.get("/api/v1/reflection/weekly-report")
}

// EvolutionList returns pending persona evolutions.
func (c *Client) EvolutionList() (map[string]any, error) {
	return c.get("/api/v1/persona/evolution")
}

// EvolutionApprove approves a persona evolution.
func (c *Client) EvolutionApprove(id string) (map[string]any, error) {
	return c.post("/api/v1/persona/evolution/"+url.PathEscape(id)+"/approve", nil)
}

// EvolutionReject rejects a persona evolution.
func (c *Client) EvolutionReject(id string) (map[string]any, error) {
	return c.post("/api/v1/persona/evolution/"+url.PathEscape(id)+"/reject", nil)
}

// ChannelsList returns active channels.
func (c *Client) ChannelsList() (map[string]any, error) {
	return c.get("/api/v1/channels")
}

// ProfileList returns all profiles with preset status.
func (c *Client) ProfileList() (map[string]any, error) {
	return c.get("/api/v1/profiles")
}

// ProfileActivate activates a profile by ID, optionally applying a preset first.
func (c *Client) ProfileActivate(id, presetID string) (map[string]any, error) {
	var body any
	if presetID != "" {
		body = map[string]string{"preset_id": presetID}
	}
	return c.post("/api/v1/profiles/"+url.PathEscape(id)+"/activate", body)
}

// TeachApprove saves a generated skill after user approval.
func (c *Client) TeachApprove(name, description, code, trigger, schedule string) (map[string]any, error) {
	return c.post("/api/v1/skills/teach/approve", map[string]string{
		"name":        name,
		"description": description,
		"code":        code,
		"trigger":     trigger,
		"schedule":    schedule,
	})
}

func (c *Client) get(path string) (map[string]any, error) {
	req, err := http.NewRequest("GET", c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	return c.do(req)
}

func (c *Client) post(path string, body any) (map[string]any, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest("POST", c.baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.do(req)
}

func (c *Client) delete(path string) (map[string]any, error) {
	req, err := http.NewRequest("DELETE", c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	return c.do(req)
}

func (c *Client) do(req *http.Request) (map[string]any, error) {
	if c.apiKey != "" {
		req.Header.Set("x-api-key", c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("server error %d: %s", resp.StatusCode, string(data))
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		// Some endpoints (e.g., /channels) return arrays. Wrap for consistency.
		var arr []any
		if json.Unmarshal(data, &arr) == nil {
			return map[string]any{"items": arr}, nil
		}
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return result, nil
}
