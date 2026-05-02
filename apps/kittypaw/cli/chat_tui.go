package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
	"github.com/mattn/go-runewidth"

	"github.com/jinto/kittypaw/client"
)

type chatTurn struct {
	ID   string
	Text string
}

type chatTurnResultMsg struct {
	Text string
	Err  error
}

type chatMessage struct {
	Role string
	Text string
}

type chatTUIOptions struct {
	Header      string
	Warning     string
	Send        func(chatTurn) tea.Cmd
	NewTurnID   func() string
	HistoryPath string
	CursorState *chatTUICursorState
}

type chatTUIModel struct {
	header  string
	warning string

	viewport viewport.Model
	input    textinput.Model
	ready    bool
	width    int
	height   int

	messages   []chatMessage
	queue      []chatTurn
	inFlight   bool
	currentPaw int

	send      func(chatTurn) tea.Cmd
	newTurnID func() string
	history   *chatInputHistory
	cursor    *chatTUICursorState
}

func newChatTUIModel(opts chatTUIOptions) chatTUIModel {
	input := textinput.New()
	input.Prompt = "you> "
	input.Placeholder = ""
	input.CharLimit = 0
	input.Width = 80
	input.SetCursorMode(textinput.CursorHide)
	input.Focus()

	send := opts.Send
	if send == nil {
		send = func(chatTurn) tea.Cmd { return nil }
	}
	newTurnID := opts.NewTurnID
	if newTurnID == nil {
		newTurnID = uuid.NewString
	}

	return chatTUIModel{
		header:     opts.Header,
		warning:    opts.Warning,
		viewport:   viewport.New(80, 20),
		input:      input,
		currentPaw: -1,
		send:       send,
		newTurnID:  newTurnID,
		history:    loadChatInputHistory(opts.HistoryPath),
		cursor:     opts.CursorState,
	}
}

func (m chatTUIModel) Init() tea.Cmd {
	return tea.ShowCursor
}

func (m chatTUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.setSize(msg.Width, msg.Height)
		return m, nil
	case chatTurnResultMsg:
		cmd := m.finishTurn(msg)
		return m, cmd
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			return m, tea.Quit
		case "enter":
			return m, m.submitInput()
		case "up":
			if m.history != nil {
				m.input.SetValue(m.history.Prev(m.input.Value()))
				m.input.CursorEnd()
				return m, nil
			}
		case "down":
			if m.history != nil {
				m.input.SetValue(m.history.Next())
				m.input.CursorEnd()
				return m, nil
			}
		}
	}

	var cmd tea.Cmd
	m.viewport, _ = m.viewport.Update(msg)
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m chatTUIModel) View() string {
	if !m.ready {
		return m.header + "\n\nStarting chat..."
	}

	header := m.header
	if m.warning != "" {
		header += "\n" + m.warning
	}

	footer := m.footerView()
	m.trackTerminalCursor()
	return header + "\n" + m.viewport.View() + "\n" + footer
}

func (m *chatTUIModel) setSize(width, height int) {
	if width < 12 {
		width = 12
	}
	if height < 4 {
		height = 4
	}
	m.ready = true
	m.width = width
	m.height = height

	headerLines := 1
	if m.warning != "" {
		headerLines++
	}
	footerLines := 2
	viewportHeight := height - headerLines - footerLines
	if viewportHeight < 1 {
		viewportHeight = 1
	}

	m.viewport.Width = width
	m.viewport.Height = viewportHeight
	m.input.Width = max(8, width-runewidth.StringWidth(m.input.Prompt)-1)
	m.refreshViewport()
}

func (m *chatTUIModel) submitInput() tea.Cmd {
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		return nil
	}
	m.input.Reset()
	if m.history != nil {
		m.history.Add(text)
	}

	turn := chatTurn{ID: m.newTurnID(), Text: text}
	m.messages = append(m.messages, chatMessage{Role: "you", Text: text})
	if m.inFlight {
		m.queue = append(m.queue, turn)
		m.refreshViewport()
		return nil
	}
	return m.startTurn(turn)
}

func (m *chatTUIModel) startTurn(turn chatTurn) tea.Cmd {
	m.inFlight = true
	m.messages = append(m.messages, chatMessage{Role: "paw", Text: "..."})
	m.currentPaw = len(m.messages) - 1
	m.refreshViewport()
	return m.send(turn)
}

func (m *chatTUIModel) finishTurn(result chatTurnResultMsg) tea.Cmd {
	text := strings.TrimSpace(result.Text)
	if result.Err != nil && text == "" {
		text = fmt.Sprintf("error: %v", result.Err)
	}
	if text == "" {
		text = "(empty response)"
	}
	if m.currentPaw >= 0 && m.currentPaw < len(m.messages) {
		m.messages[m.currentPaw].Text = text
	}
	m.inFlight = false
	m.currentPaw = -1

	if len(m.queue) > 0 {
		next := m.queue[0]
		m.queue = m.queue[1:]
		return m.startTurn(next)
	}
	m.refreshViewport()
	return nil
}

func (m *chatTUIModel) refreshViewport() {
	m.viewport.SetContent(formatChatTranscript(m.messages, m.viewport.Width))
	m.viewport.GotoBottom()
}

func (m chatTUIModel) footerView() string {
	line := strings.Repeat("-", max(0, m.width))
	return line + "\n" + m.input.View()
}

func (m chatTUIModel) trackTerminalCursor() {
	if m.cursor == nil || !m.ready {
		return
	}
	m.cursor.setPosition(m.height, chatInputCursorColumn(m.input))
}

func chatInputCursorColumn(input textinput.Model) int {
	value := []rune(input.Value())
	pos := input.Position()
	if pos < 0 {
		pos = 0
	}
	if pos > len(value) {
		pos = len(value)
	}

	start := 0
	if input.Width > 0 && runewidth.StringWidth(string(value)) > input.Width {
		start = chatInputVisibleStart(value, pos, input.Width)
	}
	beforeCursor := string(value[start:pos])
	return runewidth.StringWidth(input.Prompt) + runewidth.StringWidth(beforeCursor) + 1
}

func chatInputVisibleStart(value []rune, pos, width int) int {
	if pos <= 0 || width <= 0 {
		return 0
	}
	used := 0
	for i := pos - 1; i >= 0; i-- {
		rw := runewidth.RuneWidth(value[i])
		if used+rw > width {
			return i + 1
		}
		used += rw
	}
	return 0
}

type chatTUICursorState struct {
	mu     sync.Mutex
	row    int
	col    int
	active bool
}

func (s *chatTUICursorState) setPosition(row, col int) {
	if s == nil {
		return
	}
	if row < 1 {
		row = 1
	}
	if col < 1 {
		col = 1
	}
	s.mu.Lock()
	s.row = row
	s.col = col
	s.active = true
	s.mu.Unlock()
}

func (s *chatTUICursorState) position() (int, int, bool) {
	if s == nil {
		return 0, 0, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.row, s.col, s.active
}

func (s *chatTUICursorState) sequence() string {
	row, col, ok := s.position()
	if !ok {
		return ""
	}
	return fmt.Sprintf("\x1b[?25h\x1b[%d;%dH", row, col)
}

type chatTUICursorWriter struct {
	out    io.Writer
	file   *os.File
	cursor *chatTUICursorState
}

func (w *chatTUICursorWriter) Write(p []byte) (int, error) {
	n, err := w.out.Write(p)
	if err != nil || n != len(p) {
		return n, err
	}
	if !bytes.Contains(p, []byte("\n")) {
		return n, nil
	}
	if seq := w.cursor.sequence(); seq != "" {
		if _, err := io.WriteString(w.out, seq); err != nil {
			return n, err
		}
	}
	return n, nil
}

func (w *chatTUICursorWriter) Fd() uintptr {
	if w.file == nil {
		return ^uintptr(0)
	}
	return w.file.Fd()
}

func formatChatTranscript(messages []chatMessage, width int) string {
	if width < 12 {
		width = 12
	}
	var blocks []string
	for _, msg := range messages {
		prefix := msg.Role + "> "
		blocks = append(blocks, formatChatMessage(prefix, msg.Text, width))
	}
	return strings.Join(blocks, "\n")
}

func formatChatMessage(prefix, text string, width int) string {
	if text == "" {
		text = " "
	}
	indent := strings.Repeat(" ", runewidth.StringWidth(prefix))
	var out []string
	for i, line := range strings.Split(text, "\n") {
		linePrefix := prefix
		if i > 0 {
			linePrefix = indent
		}
		out = append(out, wrapChatLine(linePrefix, line, width)...)
	}
	return strings.Join(out, "\n")
}

func wrapChatLine(prefix, text string, width int) []string {
	prefixWidth := runewidth.StringWidth(prefix)
	limit := max(1, width-prefixWidth)
	indent := strings.Repeat(" ", prefixWidth)

	var lines []string
	var current strings.Builder
	currentWidth := 0
	flush := func(p string) {
		lines = append(lines, p+current.String())
		current.Reset()
		currentWidth = 0
	}

	for _, r := range text {
		rw := runewidth.RuneWidth(r)
		if currentWidth > 0 && currentWidth+rw > limit {
			flush(prefix)
			prefix = indent
		}
		current.WriteRune(r)
		currentWidth += rw
	}
	flush(prefix)
	return lines
}

type chatInputHistory struct {
	path    string
	entries []string
	pos     int
	draft   string
}

func loadChatInputHistory(path string) *chatInputHistory {
	h := &chatInputHistory{path: path}
	if path != "" {
		if data, err := os.ReadFile(path); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if line = strings.TrimSpace(line); line != "" {
					h.entries = append(h.entries, line)
				}
			}
		}
	}
	h.pos = len(h.entries)
	return h
}

func (h *chatInputHistory) Add(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	h.entries = append(h.entries, text)
	h.pos = len(h.entries)
	h.draft = ""
	if h.path == "" {
		return
	}
	if f, err := os.OpenFile(h.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
		_, _ = fmt.Fprintln(f, text)
		_ = f.Close()
	}
}

func (h *chatInputHistory) Prev(current string) string {
	if len(h.entries) == 0 {
		return current
	}
	if h.pos == len(h.entries) {
		h.draft = current
	}
	if h.pos > 0 {
		h.pos--
	}
	return h.entries[h.pos]
}

func (h *chatInputHistory) Next() string {
	if len(h.entries) == 0 {
		return ""
	}
	if h.pos < len(h.entries) {
		h.pos++
	}
	if h.pos == len(h.entries) {
		return h.draft
	}
	return h.entries[h.pos]
}

type chatSessionManager struct {
	ctx     context.Context
	conn    *client.DaemonConn
	session *client.ChatSession
}

func (m *chatSessionManager) Close() {
	if m.session != nil {
		m.session.Close()
	}
}

func (m *chatSessionManager) sendCmd(turn chatTurn) tea.Cmd {
	return func() tea.Msg {
		return m.sendTurn(turn)
	}
}

func (m *chatSessionManager) sendTurn(turn chatTurn) chatTurnResultMsg {
	result, gotResult, sendErr := m.sendOnce(turn)
	if sendErr != nil && !gotResult && isTransportDropErr(sendErr) {
		if m.session != nil {
			m.session.Close()
		}
		cs, err := client.DialChat(m.ctx, m.conn.WebSocketURL(), m.conn.APIKey)
		if err != nil {
			return chatTurnResultMsg{Err: fmt.Errorf("재연결 실패: %w", err)}
		}
		m.session = cs
		result, gotResult, sendErr = m.sendOnce(turn)
	}
	if sendErr != nil && !gotResult {
		return chatTurnResultMsg{Err: sendErr}
	}
	return chatTurnResultMsg{Text: result}
}

func (m *chatSessionManager) sendOnce(turn chatTurn) (result string, gotResult bool, sendErr error) {
	opts := client.ChatOptions{
		OnDone: func(fullText string, _ *int64) {
			gotResult = true
			result = fullText
		},
		OnError: func(msg string) {
			gotResult = true
			result = msg
		},
	}
	sendErr = m.session.SendTurn(turn.Text, turn.ID, opts)
	return
}

func runInteractiveChatTUI(ctx context.Context, conn *client.DaemonConn, cs *client.ChatSession, header, warning, historyFile string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	manager := &chatSessionManager{ctx: ctx, conn: conn, session: cs}
	defer manager.Close()

	cursorState := &chatTUICursorState{}
	model := newChatTUIModel(chatTUIOptions{
		Header:      header,
		Warning:     warning,
		Send:        manager.sendCmd,
		HistoryPath: historyFile,
		CursorState: cursorState,
	})
	output := &chatTUICursorWriter{out: os.Stdout, file: os.Stdout, cursor: cursorState}
	_, err := tea.NewProgram(model, tea.WithAltScreen(), tea.WithOutput(output)).Run()
	return err
}
