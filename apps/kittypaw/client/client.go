package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

// Status returns today's execution statistics.
func (c *Client) Status() (map[string]any, error) {
	return c.get("/api/v1/status")
}

// Executions returns recent execution records.
func (c *Client) Executions(limit int) (map[string]any, error) {
	return c.get(fmt.Sprintf("/api/v1/executions?limit=%d", limit))
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
	return c.delete("/api/v1/skills/" + name)
}

// DisableSkill disables a skill by name.
func (c *Client) DisableSkill(name string) (map[string]any, error) {
	return c.post("/api/v1/skills/"+name+"/disable", nil)
}

// ConfigCheck returns configuration summary.
func (c *Client) ConfigCheck() (map[string]any, error) {
	return c.get("/api/v1/config/check")
}

// MemorySearch performs full-text search over execution history.
func (c *Client) MemorySearch(query string, limit int) (map[string]any, error) {
	return c.get(fmt.Sprintf("/api/v1/memory/search?q=%s&limit=%d", query, limit))
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
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return result, nil
}
