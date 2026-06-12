package plugin

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"sync"
	"sync/atomic"
)

type JSONRPCHandler func(context.Context, string, json.RawMessage) (json.RawMessage, error)

type JSONRPCPeer struct {
	writeMu sync.Mutex
	writer  io.Writer
	handler JSONRPCHandler

	nextID    atomic.Uint64
	pendingMu sync.Mutex
	pending   map[string]chan jsonRPCResponse
	closed    bool
	closeErr  error
}

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func NewJSONRPCPeer(writer io.Writer, handler JSONRPCHandler) *JSONRPCPeer {
	return &JSONRPCPeer{
		writer:  writer,
		handler: handler,
		pending: map[string]chan jsonRPCResponse{},
	}
}

func (p *JSONRPCPeer) Serve(ctx context.Context, reader io.Reader) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		if err := p.handleLine(ctx, line); err != nil {
			p.CloseWithError(err)
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		p.CloseWithError(err)
		return err
	}
	p.CloseWithError(io.EOF)
	return nil
}

func (p *JSONRPCPeer) Call(ctx context.Context, method string, params any, result any) error {
	if p == nil {
		return fmt.Errorf("jsonrpc peer is nil")
	}
	id := strconv.FormatUint(p.nextID.Add(1), 10)
	payload, err := json.Marshal(params)
	if err != nil {
		return err
	}
	ch := make(chan jsonRPCResponse, 1)
	if err := p.addPending(id, ch); err != nil {
		return err
	}
	defer p.removePending(id)
	req := jsonRPCRequest{JSONRPC: "2.0", ID: id, Method: method, Params: payload}
	if err := p.writeJSON(req); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case resp := <-ch:
		if resp.Error != nil {
			return fmt.Errorf("jsonrpc %s: %s", method, resp.Error.Message)
		}
		if result == nil || len(resp.Result) == 0 {
			return nil
		}
		if err := json.Unmarshal(resp.Result, result); err != nil {
			return fmt.Errorf("decode jsonrpc %s result: %w", method, err)
		}
		return nil
	}
}

func (p *JSONRPCPeer) addPending(id string, ch chan jsonRPCResponse) error {
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()
	if p.closed {
		if p.closeErr != nil {
			return p.closeErr
		}
		return io.ErrClosedPipe
	}
	p.pending[id] = ch
	return nil
}

func (p *JSONRPCPeer) removePending(id string) {
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()
	delete(p.pending, id)
}

func (p *JSONRPCPeer) handleLine(ctx context.Context, line []byte) error {
	var envelope struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      string          `json:"id,omitempty"`
		Method  string          `json:"method,omitempty"`
		Params  json.RawMessage `json:"params,omitempty"`
		Result  json.RawMessage `json:"result,omitempty"`
		Error   *jsonRPCError   `json:"error,omitempty"`
	}
	if err := json.Unmarshal(line, &envelope); err != nil {
		return fmt.Errorf("decode jsonrpc line: %w", err)
	}
	if envelope.JSONRPC != "2.0" {
		return fmt.Errorf("unsupported jsonrpc version %q", envelope.JSONRPC)
	}
	if envelope.Method != "" {
		go p.handleRequest(ctx, jsonRPCRequest{JSONRPC: envelope.JSONRPC, ID: envelope.ID, Method: envelope.Method, Params: envelope.Params})
		return nil
	}
	if envelope.ID == "" {
		return fmt.Errorf("jsonrpc response missing id")
	}
	p.pendingMu.Lock()
	ch := p.pending[envelope.ID]
	p.pendingMu.Unlock()
	if ch == nil {
		return nil
	}
	ch <- jsonRPCResponse{JSONRPC: envelope.JSONRPC, ID: envelope.ID, Result: envelope.Result, Error: envelope.Error}
	return nil
}

func (p *JSONRPCPeer) handleRequest(ctx context.Context, req jsonRPCRequest) {
	var result json.RawMessage
	var rpcErr *jsonRPCError
	if p.handler == nil {
		rpcErr = &jsonRPCError{Code: -32601, Message: "host handler is not configured"}
	} else {
		value, err := p.handler(ctx, req.Method, req.Params)
		if err != nil {
			rpcErr = &jsonRPCError{Code: -32000, Message: err.Error()}
		} else {
			result = value
			if result == nil {
				result = json.RawMessage(`null`)
			}
		}
	}
	if req.ID == "" {
		return
	}
	_ = p.writeJSON(jsonRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: result, Error: rpcErr})
}

func (p *JSONRPCPeer) writeJSON(value any) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = p.writer.Write(data)
	return err
}

func (p *JSONRPCPeer) CloseWithError(err error) {
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()
	if p.closed {
		return
	}
	p.closed = true
	p.closeErr = err
	for id, ch := range p.pending {
		delete(p.pending, id)
		ch <- jsonRPCResponse{JSONRPC: "2.0", ID: id, Error: &jsonRPCError{Code: -32099, Message: err.Error()}}
	}
}
