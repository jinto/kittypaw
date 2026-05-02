package relay

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestKakaoPayloadDeserializes(t *testing.T) {
	raw := []byte(`{
		"action": {"id": "act_123"},
		"userRequest": {
			"utterance": "hello",
			"user": {"id": "user_456"},
			"callbackUrl": "https://callback.kakao.com/abc"
		}
	}`)

	var payload KakaoPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.Action.ID != "act_123" {
		t.Fatalf("Action.ID = %q", payload.Action.ID)
	}
	if payload.UserRequest.Utterance != "hello" {
		t.Fatalf("Utterance = %q", payload.UserRequest.Utterance)
	}
	if payload.UserRequest.User.ID != "user_456" {
		t.Fatalf("User.ID = %q", payload.UserRequest.User.ID)
	}
	if payload.UserRequest.CallbackURL == nil || *payload.UserRequest.CallbackURL != "https://callback.kakao.com/abc" {
		t.Fatalf("CallbackURL = %v", payload.UserRequest.CallbackURL)
	}
}

func TestKakaoPayloadWithoutCallback(t *testing.T) {
	raw := []byte(`{
		"action": {"id": "act_123"},
		"userRequest": {
			"utterance": "hello",
			"user": {"id": "user_456"}
		}
	}`)

	var payload KakaoPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.UserRequest.CallbackURL != nil {
		t.Fatalf("CallbackURL = %v, want nil", *payload.UserRequest.CallbackURL)
	}
}

func TestWSOutgoingSerializesSnakeCase(t *testing.T) {
	frame := WSOutgoing{ID: "act_123", Text: "hello", UserID: "user_456"}

	raw, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("marshal frame: %v", err)
	}
	got := string(raw)
	if !strings.Contains(got, `"user_id"`) {
		t.Fatalf("frame missing user_id: %s", got)
	}
	if strings.Contains(got, `"userId"`) {
		t.Fatalf("frame contains camelCase userId: %s", got)
	}
}

func TestWSIncomingDeserializesImageFields(t *testing.T) {
	raw := []byte(`{"id":"act_123","text":"response","image_url":"https://cdn.example.com/cat.png","image_alt":"cat"}`)

	var frame WSIncoming
	if err := json.Unmarshal(raw, &frame); err != nil {
		t.Fatalf("decode frame: %v", err)
	}
	if frame.ID != "act_123" || frame.Text != "response" {
		t.Fatalf("frame = %+v", frame)
	}
	if frame.ImageURL != "https://cdn.example.com/cat.png" {
		t.Fatalf("ImageURL = %q", frame.ImageURL)
	}
	if frame.ImageAlt != "cat" {
		t.Fatalf("ImageAlt = %q", frame.ImageAlt)
	}
}

func TestKakaoTextSerializesSimpleText(t *testing.T) {
	raw, err := json.Marshal(Text("테스트 메시지"))
	if err != nil {
		t.Fatalf("marshal text: %v", err)
	}
	got := string(raw)
	if !strings.Contains(got, `"simpleText"`) {
		t.Fatalf("missing simpleText: %s", got)
	}
	if !strings.Contains(got, `"테스트 메시지"`) {
		t.Fatalf("missing text: %s", got)
	}
	if !strings.Contains(got, `"version":"2.0"`) {
		t.Fatalf("missing version: %s", got)
	}
}

func TestKakaoImageSerializesSimpleImage(t *testing.T) {
	raw, err := json.Marshal(Image("https://cdn.example.com/cat.png", "cat"))
	if err != nil {
		t.Fatalf("marshal image: %v", err)
	}
	got := string(raw)
	if !strings.Contains(got, `"simpleImage"`) {
		t.Fatalf("missing simpleImage: %s", got)
	}
	if !strings.Contains(got, `"imageUrl":"https://cdn.example.com/cat.png"`) {
		t.Fatalf("missing imageUrl: %s", got)
	}
	if !strings.Contains(got, `"altText":"cat"`) {
		t.Fatalf("missing altText: %s", got)
	}
}

func TestKakaoAsyncAckUsesCamelCase(t *testing.T) {
	raw, err := json.Marshal(AsyncAck())
	if err != nil {
		t.Fatalf("marshal ack: %v", err)
	}
	got := string(raw)
	if !strings.Contains(got, `"useCallback":true`) {
		t.Fatalf("missing useCallback: %s", got)
	}
	if strings.Contains(got, `"use_callback"`) {
		t.Fatalf("contains snake_case use_callback: %s", got)
	}
	if !strings.Contains(got, MsgProcessing) {
		t.Fatalf("missing processing message: %s", got)
	}
}
