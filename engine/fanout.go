package engine

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jinto/kittypaw/core"
)

// executeFanout dispatches Fanout.send / Fanout.broadcast to Session.Fanout.
// The sandbox-level binding is already gated on exposeFanout; the nil check
// below is a second gate so a future sandbox refactor that accidentally
// exposes the global still can't reach a nil receiver.
func executeFanout(ctx context.Context, call core.SkillCall, s *Session) (string, error) {
	if s.Fanout == nil {
		return jsonResult(map[string]any{"error": "Fanout is not available for this tenant"})
	}

	parsePayload := func(idx int) (core.FanoutPayload, error) {
		var p core.FanoutPayload
		if len(call.Args) <= idx {
			return p, fmt.Errorf("payload argument required")
		}
		if err := json.Unmarshal(call.Args[idx], &p); err != nil {
			return p, fmt.Errorf("payload must be an object: %w", err)
		}
		if p.Text == "" {
			return p, fmt.Errorf("payload.text is required")
		}
		return p, nil
	}

	switch call.Method {
	case "send":
		if len(call.Args) < 2 {
			return jsonResult(map[string]any{"error": "Fanout.send(tenantID, payload) requires two arguments"})
		}
		var tenantID string
		if err := json.Unmarshal(call.Args[0], &tenantID); err != nil {
			return jsonResult(map[string]any{"error": "tenantID must be a string"})
		}
		payload, err := parsePayload(1)
		if err != nil {
			return jsonResult(map[string]any{"error": err.Error()})
		}
		if err := s.Fanout.Send(ctx, tenantID, payload); err != nil {
			return jsonResult(map[string]any{"error": err.Error()})
		}
		return jsonResult(map[string]any{"success": true})

	case "broadcast":
		payload, err := parsePayload(0)
		if err != nil {
			return jsonResult(map[string]any{"error": err.Error()})
		}
		if err := s.Fanout.Broadcast(ctx, payload); err != nil {
			return jsonResult(map[string]any{"error": err.Error()})
		}
		return jsonResult(map[string]any{"success": true})

	default:
		return jsonResult(map[string]any{"error": fmt.Sprintf("unknown Fanout method: %s", call.Method)})
	}
}
