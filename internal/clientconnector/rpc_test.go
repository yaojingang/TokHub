package clientconnector

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

type testWriteCloser struct {
	bytes.Buffer
}

func (*testWriteCloser) Close() error { return nil }

func TestStdioRPCReportsServerInitiatedCapabilityRequest(t *testing.T) {
	stdin := &testWriteCloser{}
	transport := &stdioRPC{
		command: &exec.Cmd{},
		stdin:   stdin,
		encoder: json.NewEncoder(stdin),
		pending: map[int64]chan rpcReply{},
		events:  make(chan map[string]any, 2),
		done:    make(chan struct{}),
	}

	transport.readLoop(strings.NewReader(`{"jsonrpc":"2.0","id":7,"method":"session/request_permission","params":{"tool":"terminal"}}` + "\n"))

	event, ok := <-transport.events
	if !ok || !forbiddenClientEvent(event) {
		t.Fatalf("server-initiated capability request was not surfaced as a blocked event: %#v", event)
	}
	if !strings.Contains(stdin.String(), `"code":-32601`) {
		t.Fatalf("server-initiated request was not denied: %s", stdin.String())
	}
}

func TestStdioRPCPrioritizesCapabilityViolationOverBufferedOutput(t *testing.T) {
	stdin := &testWriteCloser{}
	transport := &stdioRPC{
		command: &exec.Cmd{},
		stdin:   stdin,
		encoder: json.NewEncoder(stdin),
		pending: map[int64]chan rpcReply{},
		events:  make(chan map[string]any, 2),
		done:    make(chan struct{}),
	}
	transport.events <- map[string]any{"method": "turn/output_text/delta", "params": map[string]any{"delta": "one"}}
	transport.events <- map[string]any{"method": "turn/output_text/delta", "params": map[string]any{"delta": "two"}}

	transport.readLoop(strings.NewReader(`{"jsonrpc":"2.0","id":8,"method":"session/request_permission","params":{"tool":"terminal"}}` + "\n"))

	event, ok := <-transport.events
	if !ok || !forbiddenClientEvent(event) {
		t.Fatalf("policy violation was not prioritized over buffered output: %#v", event)
	}
}
