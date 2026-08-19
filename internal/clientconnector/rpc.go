package clientconnector

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"sync"
)

type RPC interface {
	Call(context.Context, string, any) (json.RawMessage, error)
	Notify(string, any) error
	Events() <-chan map[string]any
	Close() error
}

type RPCFactory interface {
	Start(context.Context, string, ...string) (RPC, error)
}

type StdioRPCFactory struct{}

func (StdioRPCFactory) Start(ctx context.Context, name string, args ...string) (RPC, error) {
	command := exec.CommandContext(ctx, name, args...)
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := command.Start(); err != nil {
		return nil, err
	}
	transport := &stdioRPC{
		command: command,
		stdin:   stdin,
		encoder: json.NewEncoder(stdin),
		pending: map[int64]chan rpcReply{},
		events:  make(chan map[string]any, 128),
		done:    make(chan struct{}),
	}
	go transport.readLoop(stdout)
	return transport, nil
}

type rpcReply struct {
	result json.RawMessage
	err    error
}

type stdioRPC struct {
	command   *exec.Cmd
	stdin     io.WriteCloser
	encoder   *json.Encoder
	writeMu   sync.Mutex
	mu        sync.Mutex
	nextID    int64
	pending   map[int64]chan rpcReply
	events    chan map[string]any
	done      chan struct{}
	closeOnce sync.Once
}

func (r *stdioRPC) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	r.mu.Lock()
	r.nextID++
	id := r.nextID
	response := make(chan rpcReply, 1)
	r.pending[id] = response
	r.mu.Unlock()
	if err := r.write(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		r.removePending(id)
		return nil, err
	}
	select {
	case reply := <-response:
		return reply.result, reply.err
	case <-ctx.Done():
		r.removePending(id)
		return nil, ctx.Err()
	case <-r.done:
		return nil, errors.New("official client RPC process stopped")
	}
}

func (r *stdioRPC) Notify(method string, params any) error {
	return r.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (r *stdioRPC) Events() <-chan map[string]any { return r.events }

func (r *stdioRPC) Close() error {
	var err error
	r.closeOnce.Do(func() {
		_ = r.stdin.Close()
		if r.command.Process != nil {
			err = r.command.Process.Kill()
		}
		_ = r.command.Wait()
		close(r.done)
	})
	return err
}

func (r *stdioRPC) write(value any) error {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	return r.encoder.Encode(value)
}

func (r *stdioRPC) readLoop(stdout io.Reader) {
	defer close(r.events)
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var message map[string]any
		if json.Unmarshal(scanner.Bytes(), &message) != nil {
			continue
		}
		if id, ok := rpcMessageID(message["id"]); ok {
			if _, isRequest := message["method"].(string); isRequest {
				_ = r.write(map[string]any{
					"jsonrpc": "2.0", "id": id,
					"error": map[string]any{"code": -32601, "message": "TokHub connector denies server-initiated capabilities"},
				})
				continue
			}
			r.mu.Lock()
			pending := r.pending[id]
			delete(r.pending, id)
			r.mu.Unlock()
			if pending != nil {
				if rpcError, exists := message["error"]; exists {
					pending <- rpcReply{err: fmt.Errorf("official client RPC error: %v", rpcError)}
				} else {
					result, _ := json.Marshal(message["result"])
					pending <- rpcReply{result: result}
				}
			}
			continue
		}
		select {
		case r.events <- message:
		default:
			_ = r.Close()
			return
		}
	}
	r.closeOnce.Do(func() {
		_ = r.stdin.Close()
		if r.command.Process != nil {
			_ = r.command.Process.Kill()
		}
		_ = r.command.Wait()
		close(r.done)
	})
}

func (r *stdioRPC) removePending(id int64) {
	r.mu.Lock()
	delete(r.pending, id)
	r.mu.Unlock()
}

func rpcMessageID(value any) (int64, bool) {
	switch typed := value.(type) {
	case float64:
		return int64(typed), true
	case json.Number:
		id, err := typed.Int64()
		return id, err == nil
	case string:
		id, err := strconv.ParseInt(typed, 10, 64)
		return id, err == nil
	default:
		return 0, false
	}
}
