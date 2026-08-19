package clientconnector

import (
	"context"
	"encoding/json"
	"reflect"
	"sync"
	"testing"
)

type fakeRPCCall struct {
	method string
	params any
}

type fakeRPC struct {
	mu        sync.Mutex
	responses map[string][]json.RawMessage
	errors    map[string]error
	events    chan map[string]any
	calls     []fakeRPCCall
}

func newFakeRPC(responses map[string][]string, events ...map[string]any) *fakeRPC {
	rpc := &fakeRPC{responses: map[string][]json.RawMessage{}, errors: map[string]error{}, events: make(chan map[string]any, len(events)+1)}
	for method, items := range responses {
		for _, item := range items {
			rpc.responses[method] = append(rpc.responses[method], json.RawMessage(item))
		}
	}
	for _, event := range events {
		rpc.events <- event
	}
	return rpc
}

func (f *fakeRPC) Call(_ context.Context, method string, params any) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fakeRPCCall{method: method, params: params})
	if err := f.errors[method]; err != nil {
		return nil, err
	}
	items := f.responses[method]
	if len(items) == 0 {
		return json.RawMessage(`{}`), nil
	}
	f.responses[method] = items[1:]
	return items[0], nil
}

func (f *fakeRPC) Notify(method string, params any) error {
	f.mu.Lock()
	f.calls = append(f.calls, fakeRPCCall{method: method, params: params})
	f.mu.Unlock()
	return nil
}

func (f *fakeRPC) Events() <-chan map[string]any { return f.events }
func (f *fakeRPC) Close() error                  { return nil }

type fakeRPCFactory struct {
	rpc  RPC
	name string
	args []string
}

func (f *fakeRPCFactory) Start(_ context.Context, name string, args ...string) (RPC, error) {
	f.name, f.args = name, append([]string{}, args...)
	return f.rpc, nil
}

func TestCodexDriverIdentifiesAccountAndModels(t *testing.T) {
	rpc := newFakeRPC(map[string][]string{
		"initialize":   {`{"userAgent":"codex-test"}`},
		"account/read": {`{"account":{"type":"chatgpt","email":"owner@example.com","planType":"plus"},"requiresOpenaiAuth":true}`},
		"model/list":   {`{"data":[{"id":"gpt-5.3-codex"},{"id":"gpt-5.4"}]}`},
	})
	factory := &fakeRPCFactory{rpc: rpc}
	result := (CodexDriver{Factory: factory}).Identify(context.Background())
	if !result.OK || result.AccountID != "owner@example.com" || result.IdentityAssurance != "account" {
		t.Fatalf("unexpected identity: %+v", result)
	}
	if !reflect.DeepEqual(result.Models, []string{"gpt-5.3-codex", "gpt-5.4"}) {
		t.Fatalf("unexpected models: %#v", result.Models)
	}
	if factory.name != "codex" || factory.args[len(factory.args)-1] != "app-server" {
		t.Fatalf("unexpected Codex command: %s %#v", factory.name, factory.args)
	}
}

func TestCodexDriverBlocksToolEvent(t *testing.T) {
	rpc := newFakeRPC(map[string][]string{
		"initialize":   {`{}`},
		"account/read": {`{"account":{"type":"chatgpt","email":"owner@example.com","planType":"plus"}}`},
		"thread/start": {`{"thread":{"id":"thr_1"}}`},
		"turn/start":   {`{"turn":{"id":"turn_1","status":"inProgress"}}`},
	}, map[string]any{"method": "item/started", "params": map[string]any{"item": map[string]any{"type": "command_execution"}}})
	result := (CodexDriver{Factory: &fakeRPCFactory{rpc: rpc}}).Generate(context.Background(), TaskPayload{Prompt: "hello"})
	if result.OK || !result.ToolEvent || result.ErrorCode != "tool_event" {
		t.Fatalf("tool event was not blocked: %+v", result)
	}
}

func TestCodexDriverRejectsImmediateFailedTurn(t *testing.T) {
	rpc := newFakeRPC(map[string][]string{
		"initialize":   {`{}`},
		"account/read": {`{"account":{"type":"chatgpt","email":"owner@example.com","planType":"plus"}}`},
		"thread/start": {`{"thread":{"id":"thr_1"}}`},
		"turn/start":   {`{"turn":{"id":"turn_1","status":"failed","output_text":"partial"}}`},
	})
	result := (CodexDriver{Factory: &fakeRPCFactory{rpc: rpc}}).Generate(context.Background(), TaskPayload{Prompt: "hello"})
	if result.OK || result.ErrorCode != "client_error" {
		t.Fatalf("failed Codex turn was accepted: %+v", result)
	}
}

func TestCodexDriverBlocksToolCallReturnedWithTurn(t *testing.T) {
	rpc := newFakeRPC(map[string][]string{
		"initialize":   {`{}`},
		"account/read": {`{"account":{"type":"chatgpt","email":"owner@example.com","planType":"plus"}}`},
		"thread/start": {`{"thread":{"id":"thr_1"}}`},
		"turn/start":   {`{"turn":{"id":"turn_1","status":"completed","items":[{"type":"tool_call","name":"shell"}]}}`},
	})
	result := (CodexDriver{Factory: &fakeRPCFactory{rpc: rpc}}).Generate(context.Background(), TaskPayload{Prompt: "hello"})
	if result.OK || !result.ToolEvent || result.ErrorCode != "tool_event" {
		t.Fatalf("inline Codex tool call was not blocked: %+v", result)
	}
}

func TestForbiddenClientValueAllowsExplicitlyDisabledCapabilities(t *testing.T) {
	value := map[string]any{
		"web_search": false,
		"tool_calls": []any{},
		"config":     map[string]any{"terminal": "disabled"},
	}
	if forbiddenClientValue(value) {
		t.Fatal("disabled capabilities were treated as active")
	}
}

func TestCodexDriverStreamsTextAndReturnsResumableThread(t *testing.T) {
	rpc := newFakeRPC(map[string][]string{
		"initialize":   {`{}`},
		"account/read": {`{"account":{"type":"chatgpt","email":"owner@example.com","planType":"plus"}}`},
		"thread/start": {`{"thread":{"id":"thr_stream"}}`},
		"turn/start":   {`{"turn":{"id":"turn_stream","status":"inProgress"}}`},
	},
		map[string]any{"method": "turn/output_text/delta", "params": map[string]any{"delta": "hello"}},
		map[string]any{"method": "turn/completed", "params": map[string]any{"turn": map[string]any{"status": "completed"}}},
	)
	result := (CodexDriver{Factory: &fakeRPCFactory{rpc: rpc}}).Generate(context.Background(), TaskPayload{Prompt: "hello", Stream: true})
	if !result.OK || result.Content != "hello" || result.SessionRef != "thr_stream" {
		t.Fatalf("unexpected streamed Codex result: %+v", result)
	}
}

func TestGrokDriverUsesCachedLoginAndLoadsPreviousSession(t *testing.T) {
	t.Setenv("TOKHUB_CLIENT_CONNECTOR_HOME", t.TempDir())
	rpc := newFakeRPC(map[string][]string{
		"initialize":     {`{"protocolVersion":1,"authMethods":[{"id":"cached_token"}]}`},
		"authenticate":   {`{}`},
		"session/load":   {`{"sessionId":"session_1"}`},
		"session/prompt": {`{"text":"hello from grok"}`},
	})
	factory := &fakeRPCFactory{rpc: rpc}
	result := (GrokDriver{Factory: factory}).Generate(context.Background(), TaskPayload{Prompt: "hello", PreviousSessionRef: "session_1"})
	if !result.OK || result.Content != "hello from grok" || result.SessionRef != "session_1" {
		t.Fatalf("unexpected Grok result: %+v", result)
	}
	if !reflect.DeepEqual(factory.args, []string{"agent", "stdio"}) {
		t.Fatalf("unexpected Grok command: %#v", factory.args)
	}
	methods := make([]string, 0, len(rpc.calls))
	for _, call := range rpc.calls {
		methods = append(methods, call.method)
	}
	if !reflect.DeepEqual(methods, []string{"initialize", "authenticate", "session/load", "session/prompt"}) {
		t.Fatalf("unexpected Grok RPC sequence: %#v", methods)
	}
	authParams := rpc.calls[1].params.(map[string]any)
	if authParams["methodId"] != "cached_token" {
		t.Fatalf("unexpected auth method: %#v", authParams)
	}
}

func TestGrokDriverBlocksToolEvent(t *testing.T) {
	t.Setenv("TOKHUB_CLIENT_CONNECTOR_HOME", t.TempDir())
	rpc := newFakeRPC(map[string][]string{
		"initialize":     {`{"protocolVersion":1,"authMethods":[{"id":"cached_token"}]}`},
		"authenticate":   {`{}`},
		"session/new":    {`{"sessionId":"session_tool"}`},
		"session/prompt": {`{"text":"partial"}`},
	}, map[string]any{"method": "session/update", "params": map[string]any{"update": map[string]any{"sessionUpdate": "tool_call"}}})
	result := (GrokDriver{Factory: &fakeRPCFactory{rpc: rpc}}).Generate(context.Background(), TaskPayload{Prompt: "hello", Stream: true})
	if result.OK || !result.ToolEvent || result.ErrorCode != "tool_event" {
		t.Fatalf("Grok tool event was not blocked: %+v", result)
	}
}

func TestGrokDriverBlocksToolCallReturnedWithPrompt(t *testing.T) {
	t.Setenv("TOKHUB_CLIENT_CONNECTOR_HOME", t.TempDir())
	rpc := newFakeRPC(map[string][]string{
		"initialize":     {`{"protocolVersion":1,"authMethods":[{"id":"cached_token"}]}`},
		"authenticate":   {`{}`},
		"session/new":    {`{"sessionId":"session_tool"}`},
		"session/prompt": {`{"updates":[{"sessionUpdate":"tool_call","toolCall":{"name":"web_search"}}]}`},
	})
	result := (GrokDriver{Factory: &fakeRPCFactory{rpc: rpc}}).Generate(context.Background(), TaskPayload{Prompt: "hello"})
	if result.OK || !result.ToolEvent || result.ErrorCode != "tool_event" {
		t.Fatalf("inline Grok tool call was not blocked: %+v", result)
	}
}
