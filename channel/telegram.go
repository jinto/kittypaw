package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jinto/kittypaw/core"
)

const (
	telegramAPI       = "https://api.telegram.org/bot"
	telegramFileAPI   = "https://api.telegram.org/file/bot"
	telegramMaxChunk  = 4096
	telegramPollSecs  = 30
	whisperAPI        = "https://api.openai.com/v1/audio/transcriptions"
	maxBackoff        = 60 * time.Second
	initialBackoff    = 1 * time.Second
)

// isDuplicateBotError checks if the Telegram error indicates another bot instance is running.
func isDuplicateBotError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "terminated by other getUpdates request")
}

// duplicateBotMessage returns a user-friendly message based on system locale.
func duplicateBotMessage() string {
	lang := os.Getenv("LANG")
	if lang == "" {
		lang = os.Getenv("LC_ALL")
	}
	lang = strings.ToLower(lang)

	switch {
	case strings.HasPrefix(lang, "ko"):
		return "\n  ⚠ 같은 봇 토큰으로 다른 인스턴스가 실행 중입니다.\n" +
			"    기존 프로세스를 종료한 뒤 다시 실행하세요.\n\n" +
			"    pkill -f kittypaw\n    kittypaw serve\n"
	case strings.HasPrefix(lang, "ja"):
		return "\n  ⚠ 同じボットトークンで別のインスタンスが実行中です。\n" +
			"    既存のプロセスを終了してから再実行してください。\n\n" +
			"    pkill -f kittypaw\n    kittypaw serve\n"
	default:
		return "\n  ⚠ Another instance is already running with the same bot token.\n" +
			"    Stop the existing process and try again.\n\n" +
			"    pkill -f kittypaw\n    kittypaw serve\n"
	}
}

// --- Telegram API DTOs ---

// telegramResponse wraps all Telegram Bot API responses.
type telegramResponse[T any] struct {
	OK          bool   `json:"ok"`
	Result      *T     `json:"result,omitempty"`
	Description string `json:"description,omitempty"`
}

// telegramUpdate is a single update from getUpdates.
type telegramUpdate struct {
	UpdateID      int64                  `json:"update_id"`
	Message       *telegramMessage       `json:"message,omitempty"`
	CallbackQuery *telegramCallbackQuery `json:"callback_query,omitempty"`
}

// telegramCallbackQuery is the callback from an inline keyboard button press.
type telegramCallbackQuery struct {
	ID   string        `json:"id"`
	From *telegramUser `json:"from,omitempty"`
	Data string        `json:"data"`
}

// telegramInlineKeyboardButton is a single button in an inline keyboard.
type telegramInlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
}

// telegramInlineKeyboardMarkup is the reply_markup for inline keyboards.
type telegramInlineKeyboardMarkup struct {
	InlineKeyboard [][]telegramInlineKeyboardButton `json:"inline_keyboard"`
}

// telegramMessage is the message object inside an update.
type telegramMessage struct {
	Chat  telegramChat  `json:"chat"`
	Text  string        `json:"text"`
	From  *telegramUser `json:"from,omitempty"`
	Voice *telegramVoice `json:"voice,omitempty"`
}

// telegramChat identifies a Telegram chat.
type telegramChat struct {
	ID int64 `json:"id"`
}

// telegramUser represents the sender of a message.
type telegramUser struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
}

// displayName returns a human-readable name for the user.
func (u *telegramUser) displayName() string {
	if u == nil {
		return "unknown"
	}
	if u.FirstName != "" && u.LastName != "" {
		return u.FirstName + " " + u.LastName
	}
	if u.FirstName != "" {
		return u.FirstName
	}
	if u.Username != "" {
		return u.Username
	}
	return "unknown"
}

// telegramVoice holds voice message metadata.
type telegramVoice struct {
	FileID string `json:"file_id"`
}

// telegramFile is the response from getFile.
type telegramFile struct {
	FilePath string `json:"file_path"`
}

// --- TelegramChannel ---

// TelegramChannel implements Channel and Confirmer using the Telegram Bot API.
// It uses long polling via getUpdates and raw HTTP (no SDK).
type TelegramChannel struct {
	botToken string
	client   *http.Client
	chatID   int64 // last chat_id for responses
	offset   int64 // next update_id to request
	mu       sync.Mutex
	pending  sync.Map // requestID → chan bool (for permission dialog responses)
}

// NewTelegram creates a TelegramChannel with the given bot token.
func NewTelegram(botToken string) *TelegramChannel {
	return &TelegramChannel{
		botToken: botToken,
		client: &http.Client{
			Timeout: time.Duration(telegramPollSecs+10) * time.Second,
		},
	}
}

func (t *TelegramChannel) Name() string { return "telegram" }

// Start long-polls the Telegram Bot API for updates. It blocks until
// ctx is cancelled, emitting core.Event values on eventCh.
func (t *TelegramChannel) Start(ctx context.Context, eventCh chan<- core.Event) error {
	slog.Info("telegram: starting long-poll loop")
	backoff := initialBackoff

	for {
		select {
		case <-ctx.Done():
			slog.Info("telegram: shutting down")
			return ctx.Err()
		default:
		}

		updates, err := t.getUpdates(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if isDuplicateBotError(err) {
				fmt.Fprint(os.Stderr, duplicateBotMessage())
				return fmt.Errorf("telegram: duplicate bot instance")
			}
			slog.Warn("telegram: getUpdates failed, backing off",
				"error", err, "backoff", backoff)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, maxBackoff)
			continue
		}
		backoff = initialBackoff

		for _, upd := range updates {
			t.mu.Lock()
			if upd.UpdateID >= t.offset {
				t.offset = upd.UpdateID + 1
			}
			t.mu.Unlock()

			// Route callback queries to the pending permission map
			// (bypasses eventCh to prevent deadlock with dispatchLoop).
			if upd.CallbackQuery != nil {
				t.resolveCallback(ctx, upd.CallbackQuery)
				continue
			}

			if upd.Message == nil {
				continue
			}

			msg := upd.Message
			t.mu.Lock()
			t.chatID = msg.Chat.ID
			t.mu.Unlock()

			text := msg.Text

			// Voice message: download and transcribe via Whisper.
			if msg.Voice != nil && text == "" {
				transcribed, err := t.transcribeVoice(ctx, msg.Voice.FileID)
				if err != nil {
					slog.Warn("telegram: voice transcription failed", "error", err)
					continue
				}
				text = transcribed
			}

			if text == "" {
				continue
			}

			fromName := ""
			if msg.From != nil {
				fromName = msg.From.displayName()
			}

			chatIDStr := strconv.FormatInt(msg.Chat.ID, 10)

			// Use user ID as SessionID for per-user session continuity.
			sessionID := chatIDStr
			if msg.From != nil && msg.From.ID != 0 {
				sessionID = strconv.FormatInt(msg.From.ID, 10)
			}

			payload := core.ChatPayload{
				ChatID:    chatIDStr,
				Text:      text,
				FromName:  fromName,
				SessionID: sessionID,
			}
			raw, err := json.Marshal(payload)
			if err != nil {
				slog.Error("telegram: marshal payload", "error", err)
				continue
			}

			event := core.Event{
				Type:    core.EventTelegram,
				Payload: raw,
			}

			select {
			case eventCh <- event:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
}

// SendResponse sends a text response to a Telegram chat.
// The chatIDStr parameter is the numeric chat ID as a string (from ChatPayload.ChatID).
// Falls back to the most recently cached chat ID if parsing fails.
// Long messages are split into chunks of telegramMaxChunk characters.
func (t *TelegramChannel) SendResponse(ctx context.Context, chatIDStr, response string) error {
	chatID, err := strconv.ParseInt(chatIDStr, 10, 64)
	if err != nil {
		// Fall back to cached chat ID.
		t.mu.Lock()
		chatID = t.chatID
		t.mu.Unlock()
	}

	if chatID == 0 {
		return fmt.Errorf("telegram: no chat_id to respond to")
	}

	// Send typing indicator.
	_ = t.sendChatAction(ctx, chatID, "typing")

	// Split into chunks.
	chunks := core.SplitChunks(response, telegramMaxChunk)
	for _, chunk := range chunks {
		if err := t.sendMessage(ctx, chatID, chunk); err != nil {
			return fmt.Errorf("telegram: sendMessage: %w", err)
		}
	}
	return nil
}

// --- internal helpers ---

func (t *TelegramChannel) apiURL(method string) string {
	return telegramAPI + t.botToken + "/" + method
}

func (t *TelegramChannel) getUpdates(ctx context.Context) ([]telegramUpdate, error) {
	t.mu.Lock()
	offset := t.offset
	t.mu.Unlock()

	body := map[string]any{
		"offset":  offset,
		"timeout": telegramPollSecs,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		t.apiURL("getUpdates"), bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result telegramResponse[[]telegramUpdate]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode getUpdates: %w", err)
	}
	if !result.OK {
		return nil, fmt.Errorf("getUpdates: %s", result.Description)
	}
	if result.Result == nil {
		return nil, nil
	}
	return *result.Result, nil
}

func (t *TelegramChannel) sendMessage(ctx context.Context, chatID int64, text string) error {
	body := map[string]any{
		"chat_id": chatID,
		"text":    text,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		t.apiURL("sendMessage"), bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var result telegramResponse[json.RawMessage]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode sendMessage: %w", err)
	}
	if !result.OK {
		return fmt.Errorf("sendMessage: %s", result.Description)
	}
	return nil
}

func (t *TelegramChannel) sendChatAction(ctx context.Context, chatID int64, action string) error {
	body := map[string]any{
		"chat_id": chatID,
		"action":  action,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		t.apiURL("sendChatAction"), bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// transcribeVoice downloads a Telegram voice file and sends it to
// the OpenAI Whisper API for speech-to-text transcription.
func (t *TelegramChannel) transcribeVoice(ctx context.Context, fileID string) (string, error) {
	// Step 1: get the file path from Telegram.
	filePath, err := t.getFilePath(ctx, fileID)
	if err != nil {
		return "", fmt.Errorf("getFile: %w", err)
	}

	// Step 2: download the file bytes.
	fileURL := telegramFileAPI + t.botToken + "/" + filePath
	fileReq, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return "", err
	}
	fileResp, err := t.client.Do(fileReq)
	if err != nil {
		return "", fmt.Errorf("download voice file: %w", err)
	}
	defer fileResp.Body.Close()

	audioData, err := io.ReadAll(fileResp.Body)
	if err != nil {
		return "", fmt.Errorf("read voice file: %w", err)
	}

	// Step 3: send to Whisper API.
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("OPENAI_API_KEY not set for voice transcription")
	}

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", "voice.ogg")
	if err != nil {
		return "", err
	}
	if _, err := part.Write(audioData); err != nil {
		return "", err
	}
	_ = w.WriteField("model", "whisper-1")
	_ = w.WriteField("language", "ko")
	w.Close()

	whisperReq, err := http.NewRequestWithContext(ctx, http.MethodPost, whisperAPI, &buf)
	if err != nil {
		return "", err
	}
	whisperReq.Header.Set("Authorization", "Bearer "+apiKey)
	whisperReq.Header.Set("Content-Type", w.FormDataContentType())

	whisperResp, err := t.client.Do(whisperReq)
	if err != nil {
		return "", fmt.Errorf("whisper request: %w", err)
	}
	defer whisperResp.Body.Close()

	var whisperResult struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(whisperResp.Body).Decode(&whisperResult); err != nil {
		return "", fmt.Errorf("decode whisper response: %w", err)
	}

	slog.Info("telegram: transcribed voice message",
		"length", len(audioData), "text_length", len(whisperResult.Text))
	return whisperResult.Text, nil
}

func (t *TelegramChannel) getFilePath(ctx context.Context, fileID string) (string, error) {
	body := map[string]any{"file_id": fileID}
	data, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		t.apiURL("getFile"), bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result telegramResponse[telegramFile]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode getFile: %w", err)
	}
	if !result.OK || result.Result == nil {
		return "", fmt.Errorf("getFile: %s", result.Description)
	}
	return result.Result.FilePath, nil
}

// --- Confirmer implementation ---

// AskConfirmation sends an inline keyboard with approve/deny buttons and blocks
// until the user clicks one or ctx expires. The timeout is controlled by the
// caller via context.WithTimeout — this method only listens to ctx.Done().
func (t *TelegramChannel) AskConfirmation(ctx context.Context, chatID, description, resource string) (bool, error) {
	reqID := uuid.New().String()
	ch := make(chan bool, 1)
	t.pending.Store(reqID, ch)
	defer t.pending.Delete(reqID)

	chatIDInt, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return false, fmt.Errorf("telegram: invalid chatID %q: %w", chatID, err)
	}

	if err := t.sendInlineKeyboard(ctx, chatIDInt, description, reqID); err != nil {
		return false, fmt.Errorf("telegram: send permission keyboard: %w", err)
	}

	select {
	case ok := <-ch:
		return ok, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// resolveCallback handles a callback_query by looking up the requestID
// in the pending map and sending the approval/denial to the waiting goroutine.
//
// NOTE: Currently does not verify query.From against the original requester.
// This is acceptable for personal-agent 1:1 chats (design assumption).
// If group-chat support is added, verify query.From.ID matches the requester
// to prevent unauthorized approvals.
func (t *TelegramChannel) resolveCallback(ctx context.Context, query *telegramCallbackQuery) {
	// Always acknowledge the callback to remove the loading spinner.
	t.answerCallbackQuery(ctx, query.ID)

	// Parse callback_data: "a:{reqID}" or "d:{reqID}"
	data := query.Data
	if len(data) < 3 || data[1] != ':' {
		slog.Debug("telegram: ignoring callback with unexpected format", "data", data)
		return
	}

	prefix := data[0]
	reqID := data[2:]

	val, ok := t.pending.LoadAndDelete(reqID)
	if !ok {
		// Stale or duplicate callback — the request already resolved or timed out.
		slog.Debug("telegram: no pending permission for callback", "req_id", reqID)
		return
	}

	ch := val.(chan bool)
	switch prefix {
	case 'a':
		ch <- true
	default:
		ch <- false
	}
}

// sendInlineKeyboard sends a message with an inline keyboard for permission approval.
func (t *TelegramChannel) sendInlineKeyboard(ctx context.Context, chatID int64, description, reqID string) error {
	// Truncate description to fit within Telegram's message limits.
	msg := "⚠️ Permission required:\n\n" + description + "\n\nApprove or deny?"
	if len(msg) > telegramMaxChunk {
		msg = msg[:telegramMaxChunk-3] + "..."
	}

	keyboard := telegramInlineKeyboardMarkup{
		InlineKeyboard: [][]telegramInlineKeyboardButton{
			{
				{Text: "✅ Approve", CallbackData: "a:" + reqID},
				{Text: "❌ Deny", CallbackData: "d:" + reqID},
			},
		},
	}

	body := map[string]any{
		"chat_id":      chatID,
		"text":         msg,
		"reply_markup": keyboard,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		t.apiURL("sendMessage"), bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var result telegramResponse[json.RawMessage]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode sendMessage: %w", err)
	}
	if !result.OK {
		return fmt.Errorf("sendMessage: %s", result.Description)
	}
	return nil
}

// answerCallbackQuery acknowledges a callback_query to Telegram,
// removing the loading spinner from the button.
func (t *TelegramChannel) answerCallbackQuery(ctx context.Context, callbackQueryID string) {
	body := map[string]any{"callback_query_id": callbackQueryID}
	data, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		t.apiURL("answerCallbackQuery"), bytes.NewReader(data))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}

// Compile-time check: TelegramChannel implements Confirmer.
var _ Confirmer = (*TelegramChannel)(nil)

