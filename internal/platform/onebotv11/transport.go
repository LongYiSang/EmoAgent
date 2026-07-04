package onebotv11

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type EventHandler interface {
	HandleOneBotEvent(context.Context, Event) error
}

func contextWithRequestTimeout(ctx context.Context, timeoutMS int) (context.Context, context.CancelFunc) {
	if timeoutMS <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, time.Duration(timeoutMS)*time.Millisecond)
}

type Transport interface {
	ActionClient
	Start(context.Context, EventHandler) error
	Stop(context.Context) error
	Status() TransportStatus
}

type TransportStatus struct {
	Mode      string
	State     string
	URL       string
	SelfID    string
	Connected bool
}

type universalWSConn struct {
	conn    *websocket.Conn
	echoes  *EchoStore
	handler EventHandler
	writeMu sync.Mutex
}

func newUniversalWSConn(conn *websocket.Conn, handler EventHandler) *universalWSConn {
	return &universalWSConn{
		conn:    conn,
		echoes:  newEchoStore(),
		handler: handler,
	}
}

func (c *universalWSConn) run(ctx context.Context) error {
	for {
		var raw map[string]json.RawMessage
		if err := wsjson.Read(ctx, c.conn, &raw); err != nil {
			c.echoes.failAll(fmt.Errorf("onebot websocket closed: %w", err))
			return err
		}
		if isActionResponse(raw) {
			var resp ActionResponse
			data, _ := json.Marshal(raw)
			if err := json.Unmarshal(data, &resp); err == nil {
				c.echoes.resolve(resp)
			}
			continue
		}
		var event Event
		data, _ := json.Marshal(raw)
		if err := json.Unmarshal(data, &event); err != nil {
			continue
		}
		if c.handler != nil {
			go func() {
				_ = c.handler.HandleOneBotEvent(ctx, event)
			}()
		}
	}
}

func (c *universalWSConn) Call(ctx context.Context, req ActionRequest) (ActionResponse, error) {
	if req.Echo == "" {
		req.Echo = nextEcho()
	}
	wait := c.echoes.register(req.Echo)
	c.writeMu.Lock()
	err := wsjson.Write(ctx, c.conn, req)
	c.writeMu.Unlock()
	if err != nil {
		c.echoes.remove(req.Echo)
		return ActionResponse{}, err
	}
	return wait(ctx)
}

func isActionResponse(raw map[string]json.RawMessage) bool {
	if _, ok := raw["echo"]; !ok {
		return false
	}
	_, hasStatus := raw["status"]
	_, hasRetcode := raw["retcode"]
	return hasStatus || hasRetcode
}
