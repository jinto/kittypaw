package main

import (
	"bytes"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestChatTUISubmitStartsTurnAndKeepsInputFocused(t *testing.T) {
	started := make(chan chatTurn, 1)
	model := newChatTUIModel(chatTUIOptions{
		Header: "KittyPaw chat",
		Send: func(turn chatTurn) tea.Cmd {
			started <- turn
			return func() tea.Msg { return nil }
		},
		NewTurnID: func() string { return "turn-1" },
	})
	model.setSize(60, 12)
	model.input.SetValue("haha")

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(chatTUIModel)

	if cmd == nil {
		t.Fatal("submit must start a send command")
	}
	if !model.input.Focused() {
		t.Fatal("input must stay focused after submit")
	}
	if got := model.input.Value(); got != "" {
		t.Fatalf("input value = %q, want empty after submit", got)
	}
	if !model.inFlight {
		t.Fatal("model must mark the turn in-flight")
	}
	if len(model.queue) != 0 {
		t.Fatalf("queue len = %d, want 0", len(model.queue))
	}
	transcript := formatChatTranscript(model.messages, 60)
	if !strings.Contains(transcript, "you> haha") {
		t.Fatalf("transcript missing submitted user text:\n%s", transcript)
	}
	if !strings.Contains(transcript, "paw> ...") {
		t.Fatalf("transcript missing pending paw placeholder:\n%s", transcript)
	}
	if got := <-started; got.Text != "haha" || got.ID != "turn-1" {
		t.Fatalf("started turn = %#v", got)
	}
}

func TestChatTUISubmitWhileBusyQueuesNextTurn(t *testing.T) {
	starts := 0
	model := newChatTUIModel(chatTUIOptions{
		Header: "KittyPaw chat",
		Send: func(turn chatTurn) tea.Cmd {
			starts++
			return func() tea.Msg { return nil }
		},
		NewTurnID: func() string { return "turn" },
	})
	model.setSize(60, 12)
	model.inFlight = true
	model.input.SetValue("next")

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(chatTUIModel)

	if cmd != nil {
		t.Fatal("busy submit must queue without starting another send command")
	}
	if starts != 0 {
		t.Fatalf("send starts = %d, want 0", starts)
	}
	if len(model.queue) != 1 || model.queue[0].Text != "next" {
		t.Fatalf("queue = %#v", model.queue)
	}
	transcript := formatChatTranscript(model.messages, 60)
	if !strings.Contains(transcript, "you> next") {
		t.Fatalf("transcript missing queued user text:\n%s", transcript)
	}
}

func TestChatTUIResultStartsQueuedTurn(t *testing.T) {
	var started []chatTurn
	model := newChatTUIModel(chatTUIOptions{
		Header: "KittyPaw chat",
		Send: func(turn chatTurn) tea.Cmd {
			started = append(started, turn)
			return func() tea.Msg { return nil }
		},
		NewTurnID: func() string { return "unused" },
	})
	model.setSize(60, 12)
	model.inFlight = true
	model.currentPaw = 1
	model.messages = []chatMessage{
		{Role: "you", Text: "first"},
		{Role: "paw", Text: "..."},
		{Role: "you", Text: "second"},
	}
	model.queue = []chatTurn{{ID: "turn-2", Text: "second"}}

	updated, cmd := model.Update(chatTurnResultMsg{Text: "done"})
	model = updated.(chatTUIModel)

	if cmd == nil {
		t.Fatal("queued turn must start after previous result")
	}
	if len(started) != 1 || started[0].ID != "turn-2" {
		t.Fatalf("started = %#v", started)
	}
	if len(model.queue) != 0 {
		t.Fatalf("queue len = %d, want 0", len(model.queue))
	}
	transcript := formatChatTranscript(model.messages, 60)
	if !strings.Contains(transcript, "paw> done") {
		t.Fatalf("transcript missing completed first response:\n%s", transcript)
	}
	if !strings.Contains(transcript, "paw> ...") {
		t.Fatalf("transcript missing pending second response:\n%s", transcript)
	}
}

func TestFormatChatTranscriptWrapsContinuationLines(t *testing.T) {
	got := formatChatTranscript([]chatMessage{
		{Role: "you", Text: "abcdefghijklmnop"},
	}, 12)
	want := "you> abcdefg\n     hijklmn\n     op"
	if got != want {
		t.Fatalf("wrapped transcript = %q, want %q", got, want)
	}
}

func TestChatTUIViewUsesFullTerminalHeight(t *testing.T) {
	model := newChatTUIModel(chatTUIOptions{Header: "KittyPaw chat"})
	model.setSize(40, 10)

	got := strings.Count(model.View(), "\n") + 1
	if got != 10 {
		t.Fatalf("View line count = %d, want 10", got)
	}
}

func TestChatTUIViewDoesNotExceedSmallTerminalHeight(t *testing.T) {
	model := newChatTUIModel(chatTUIOptions{Header: "KittyPaw chat"})
	model.setSize(20, 5)

	got := strings.Count(model.View(), "\n") + 1
	if got != 5 {
		t.Fatalf("small View line count = %d, want 5", got)
	}
}

func TestChatTUIViewTracksTerminalCursorAtInputCursor(t *testing.T) {
	cursor := &chatTUICursorState{}
	model := newChatTUIModel(chatTUIOptions{
		Header:      "KittyPaw chat",
		CursorState: cursor,
	})
	model.setSize(40, 10)
	model.input.SetValue("너는 누")

	_ = model.View()

	row, col, ok := cursor.position()
	if !ok {
		t.Fatal("cursor position was not tracked")
	}
	if row != 10 {
		t.Fatalf("cursor row = %d, want 10", row)
	}
	if col != 13 {
		t.Fatalf("cursor col = %d, want 13", col)
	}
}

func TestChatTUICursorWriterAppendsCursorAfterRenderedFrame(t *testing.T) {
	cursor := &chatTUICursorState{}
	cursor.setPosition(10, 13)
	var out bytes.Buffer
	writer := &chatTUICursorWriter{out: &out, cursor: cursor}

	n, err := writer.Write([]byte("frame\nline"))

	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if n != len("frame\nline") {
		t.Fatalf("Write returned n = %d, want %d", n, len("frame\nline"))
	}
	want := "frame\nline\x1b[?25h\x1b[10;13H"
	if got := out.String(); got != want {
		t.Fatalf("writer output = %q, want %q", got, want)
	}
}

func TestChatTUICursorWriterLeavesControlWritesAlone(t *testing.T) {
	cursor := &chatTUICursorState{}
	cursor.setPosition(10, 13)
	var out bytes.Buffer
	writer := &chatTUICursorWriter{out: &out, cursor: cursor}

	_, err := writer.Write([]byte("\x1b[?25l"))

	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if got := out.String(); got != "\x1b[?25l" {
		t.Fatalf("writer output = %q, want raw control write", got)
	}
}
