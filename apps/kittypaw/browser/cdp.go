package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"

	"nhooyr.io/websocket"
)

type cdpConn interface {
	Write(context.Context, []byte) error
	Read(context.Context) ([]byte, error)
	Close() error
}

type websocketConn struct {
	conn *websocket.Conn
}

func (w *websocketConn) Write(ctx context.Context, b []byte) error {
	return w.conn.Write(ctx, websocket.MessageText, b)
}

func (w *websocketConn) Read(ctx context.Context) ([]byte, error) {
	_, b, err := w.conn.Read(ctx)
	return b, err
}

func (w *websocketConn) Close() error {
	return w.conn.Close(websocket.StatusNormalClosure, "")
}

type cdpRequest struct {
	ID        int64  `json:"id"`
	Method    string `json:"method"`
	Params    any    `json:"params,omitempty"`
	SessionID string `json:"sessionId,omitempty"`
}

type cdpResponse struct {
	ID     int64           `json:"id,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *cdpError       `json:"error,omitempty"`
}

type cdpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type cdpResult struct {
	resp cdpResponse
	err  error
}

type cdpClient struct {
	conn    cdpConn
	ctx     context.Context
	cancel  context.CancelFunc
	nextID  atomic.Int64
	mu      sync.Mutex
	pending map[int64]chan cdpResult
	closed  bool
}

func newCDPClient(conn cdpConn) *cdpClient {
	ctx, cancel := context.WithCancel(context.Background())
	c := &cdpClient{
		conn:    conn,
		ctx:     ctx,
		cancel:  cancel,
		pending: make(map[int64]chan cdpResult),
	}
	go c.readLoop()
	return c
}

func (c *cdpClient) readLoop() {
	for {
		b, err := c.conn.Read(c.ctx)
		if err != nil {
			c.failAll(err)
			return
		}
		var resp cdpResponse
		if err := json.Unmarshal(b, &resp); err != nil || resp.ID == 0 {
			continue
		}
		c.mu.Lock()
		ch := c.pending[resp.ID]
		delete(c.pending, resp.ID)
		c.mu.Unlock()
		if ch != nil {
			ch <- cdpResult{resp: resp}
			close(ch)
		}
	}
}

func (c *cdpClient) Call(ctx context.Context, method string, params any, out any) error {
	return c.CallSession(ctx, "", method, params, out)
}

func (c *cdpClient) CallSession(ctx context.Context, sessionID, method string, params any, out any) error {
	id := c.nextID.Add(1)
	ch := make(chan cdpResult, 1)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return fmt.Errorf("cdp client closed")
	}
	c.pending[id] = ch
	c.mu.Unlock()

	req := cdpRequest{ID: id, Method: method, Params: params, SessionID: sessionID}
	data, err := json.Marshal(req)
	if err != nil {
		c.removePending(id)
		return err
	}
	if err := c.conn.Write(ctx, data); err != nil {
		c.removePending(id)
		return err
	}
	select {
	case result := <-ch:
		if result.err != nil {
			return result.err
		}
		if result.resp.Error != nil {
			return fmt.Errorf("cdp error %d: %s", result.resp.Error.Code, result.resp.Error.Message)
		}
		if out != nil && len(result.resp.Result) > 0 {
			return json.Unmarshal(result.resp.Result, out)
		}
		return nil
	case <-ctx.Done():
		c.removePending(id)
		return ctx.Err()
	}
}

func (c *cdpClient) removePending(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func (c *cdpClient) failAll(err error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	for id, ch := range c.pending {
		delete(c.pending, id)
		ch <- cdpResult{err: err}
		close(ch)
	}
	c.mu.Unlock()
}

func (c *cdpClient) Close() error {
	c.cancel()
	c.failAll(fmt.Errorf("cdp client closed"))
	return c.conn.Close()
}
