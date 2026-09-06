package inbound

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func serverImpl(name string) *mcp.Implementation {
	return &mcp.Implementation{
		Name:    name,
		Version: "1.0.0",
	}
}

func clientImpl(name string) *mcp.Implementation {
	return &mcp.Implementation{
		Name:    name,
		Version: "1.0.0",
	}
}

func makeValidTool(name string) *mcp.Tool {
	return &mcp.Tool{
		Name:        name,
		Description: "Tool " + name,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"param": map[string]any{"type": "string"},
			},
		},
	}
}

func TestPublisherBasicPublishAndCall(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	server := mcp.NewServer(serverImpl("test-hub"), nil)

	var callbackCalled atomic.Bool
	var calledToolName string
	var calledArgs json.RawMessage
	var cbMu sync.Mutex

	routerCb := func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		cbMu.Lock()
		calledToolName = req.Params.Name
		calledArgs = req.Params.Arguments
		cbMu.Unlock()
		callbackCalled.Store(true)

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "tool result ok"},
			},
		}, nil
	}

	pub, err := NewPublisher(server, nil, routerCb)
	if err != nil {
		t.Fatalf("NewPublisher failed: %v", err)
	}

	// Connect an in-memory client
	ct, st := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect failed: %v", err)
	}
	defer serverSession.Close()

	client := mcp.NewClient(clientImpl("test-client"), nil)
	clientSession, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect failed: %v", err)
	}
	defer clientSession.Close()

	// Initial empty list
	resList, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("initial ListTools failed: %v", err)
	}
	if len(resList.Tools) != 0 {
		t.Fatalf("expected 0 tools initially, got %d", len(resList.Tools))
	}

	// Publish tools for server "fs"
	tools := []*mcp.Tool{
		makeValidTool("read_file"),
		makeValidTool("write_file"),
	}
	snap, err := pub.PublishServer("fs", tools, nil)
	if err != nil {
		t.Fatalf("PublishServer failed: %v", err)
	}
	if snap.TotalPublished() != 2 {
		t.Errorf("expected 2 published tools in snapshot, got %d", snap.TotalPublished())
	}

	// Verify client sees tools
	resList, err = clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools after publish failed: %v", err)
	}
	if len(resList.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(resList.Tools))
	}
	if resList.Tools[0].Name != "fs__read_file" || resList.Tools[1].Name != "fs__write_file" {
		t.Errorf("unexpected tool names in client list: %s, %s", resList.Tools[0].Name, resList.Tools[1].Name)
	}

	// Call tool via client
	callParams := &mcp.CallToolParams{
		Name:      "fs__read_file",
		Arguments: map[string]any{"param": "hello"},
	}
	callRes, err := clientSession.CallTool(ctx, callParams)
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if !callbackCalled.Load() {
		t.Fatalf("expected routerCallback to be invoked")
	}

	cbMu.Lock()
	if calledToolName != "fs__read_file" {
		t.Errorf("expected called tool name 'fs__read_file', got %q", calledToolName)
	}
	if !json.Valid(calledArgs) {
		t.Errorf("invalid arguments json received: %s", string(calledArgs))
	}
	cbMu.Unlock()

	if len(callRes.Content) == 0 {
		t.Fatalf("empty content in call result")
	}
	tc, ok := callRes.Content[0].(*mcp.TextContent)
	if !ok || tc.Text != "tool result ok" {
		t.Errorf("unexpected content: %+v", callRes.Content[0])
	}
}

func TestPublisherUpdateAndRemoval(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	server := mcp.NewServer(serverImpl("test-hub"), nil)
	pub, err := NewPublisher(server, nil, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "ok"}},
		}, nil
	})
	if err != nil {
		t.Fatalf("NewPublisher failed: %v", err)
	}

	ct, st := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	client := mcp.NewClient(clientImpl("test-client"), nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	// 1. Publish tool1 and tool2
	initial := []*mcp.Tool{
		makeValidTool("tool1"),
		makeValidTool("tool2"),
	}
	_, err = pub.PublishServer("srv", initial, nil)
	if err != nil {
		t.Fatalf("initial publish failed: %v", err)
	}

	res, err := cs.ListTools(ctx, nil)
	if err != nil || len(res.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d, err=%v", len(res.Tools), err)
	}

	// 2. Update: disable tool1 via disabled list, remove tool2, add tool3
	updated := []*mcp.Tool{
		makeValidTool("tool1"),
		makeValidTool("tool3"),
	}
	disabled := []string{"tool1"}

	_, err = pub.PublishServer("srv", updated, disabled)
	if err != nil {
		t.Fatalf("update publish failed: %v", err)
	}

	res, err = cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	if len(res.Tools) != 1 {
		t.Fatalf("expected 1 tool (only tool3), got %d: %+v", len(res.Tools), res.Tools)
	}
	if res.Tools[0].Name != "srv__tool3" {
		t.Errorf("expected tool name 'srv__tool3', got %s", res.Tools[0].Name)
	}

	// 3. Attempting to call removed tool srv__tool1 should fail
	_, err = cs.CallTool(ctx, &mcp.CallToolParams{Name: "srv__tool1"})
	if err == nil {
		t.Errorf("expected calling disabled/removed tool to fail, but succeeded")
	}

	// 4. Remove server entirely
	_, err = pub.RemoveServer("srv")
	if err != nil {
		t.Fatalf("RemoveServer failed: %v", err)
	}

	res, err = cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools after remove failed: %v", err)
	}
	if len(res.Tools) != 0 {
		t.Errorf("expected 0 tools after RemoveServer, got %d", len(res.Tools))
	}
}

func TestPublisherAtomicListDuringPublish(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	server := mcp.NewServer(serverImpl("test-hub"), nil)
	pub, err := NewPublisher(server, nil, nil)
	if err != nil {
		t.Fatalf("NewPublisher failed: %v", err)
	}

	ct, st := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	client := mcp.NewClient(clientImpl("test-client"), nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	stopCh := make(chan struct{})
	var halfStateObserved atomic.Bool

	// Lister goroutine: continuously lists tools and verifies atomicity
	// Tools are always published in pairs: either {tool_a, tool_b} (Set 1) or {tool_c, tool_d} (Set 2)
	// Lister should NEVER see a half-state (e.g. 1 tool or mixed set)!
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stopCh:
				return
			default:
			}

			listRes, err := cs.ListTools(ctx, nil)
			if err != nil {
				continue
			}

			n := len(listRes.Tools)
			if n == 0 {
				// Initial state is fine
				continue
			}
			if n != 2 {
				// HALF STATE OBSERVED! Only 1 tool or 3 tools visible!
				halfStateObserved.Store(true)
				return
			}

			// Verify set consistency: both must belong to Set 1 or Set 2
			t0 := listRes.Tools[0].Name
			t1 := listRes.Tools[1].Name
			isSet1 := (t0 == "s__pair_a" && t1 == "s__pair_b") || (t0 == "s__pair_b" && t1 == "s__pair_a")
			isSet2 := (t0 == "s__pair_c" && t1 == "s__pair_d") || (t0 == "s__pair_d" && t1 == "s__pair_c")

			if !isSet1 && !isSet2 {
				halfStateObserved.Store(true)
				return
			}
		}
	}()

	// Publisher loop: rapidly alternate between Set 1 and Set 2
	set1 := []*mcp.Tool{makeValidTool("pair_a"), makeValidTool("pair_b")}
	set2 := []*mcp.Tool{makeValidTool("pair_c"), makeValidTool("pair_d")}

	for i := 0; i < 50; i++ {
		target := set1
		if i%2 == 1 {
			target = set2
		}
		_, err := pub.PublishServer("s", target, nil)
		if err != nil {
			t.Fatalf("PublishServer error at iteration %d: %v", i, err)
		}
		time.Sleep(2 * time.Millisecond)
	}

	close(stopCh)
	wg.Wait()

	if halfStateObserved.Load() {
		t.Fatalf("FAIL: tools/list middleware failed to prevent half-published state!")
	}
}

func TestPublisherInFlightCallNonBlocking(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	server := mcp.NewServer(serverImpl("test-hub"), nil)

	toolBlockCh := make(chan struct{})
	toolStartedCh := make(chan struct{})

	routerCb := func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if req.Params.Name == "srv__slow_tool" {
			close(toolStartedCh)
			select {
			case <-toolBlockCh:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "slow_done"}},
		}, nil
	}

	pub, err := NewPublisher(server, nil, routerCb)
	if err != nil {
		t.Fatalf("NewPublisher: %v", err)
	}

	// Publish slow_tool
	_, err = pub.PublishServer("srv", []*mcp.Tool{makeValidTool("slow_tool")}, nil)
	if err != nil {
		t.Fatalf("PublishServer: %v", err)
	}

	ct, st := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	client := mcp.NewClient(clientImpl("test-client"), nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	// Start in-flight tool call in background
	callDone := make(chan struct{})
	go func() {
		_, _ = cs.CallTool(ctx, &mcp.CallToolParams{Name: "srv__slow_tool"})
		close(callDone)
	}()

	// Wait until tool handler enters execution
	select {
	case <-toolStartedCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for slow_tool to start")
	}

	// While slow_tool is BLOCKED, PublishServer must NOT be blocked!
	publishDone := make(chan struct{})
	go func() {
		_, err := pub.PublishServer("srv", []*mcp.Tool{
			makeValidTool("slow_tool"),
			makeValidTool("fast_tool"),
		}, nil)
		if err != nil {
			t.Errorf("PublishServer during in-flight call failed: %v", err)
		}
		close(publishDone)
	}()

	select {
	case <-publishDone:
		// Publish completed immediately! Lock was NOT held across tool execution!
	case <-time.After(1 * time.Second):
		t.Fatal("FAIL: Publish was blocked by in-flight tool call!")
	}

	// While slow_tool is still BLOCKED, ListTools must also succeed!
	listDone := make(chan struct{})
	go func() {
		listRes, err := cs.ListTools(ctx, nil)
		if err != nil {
			t.Errorf("ListTools during in-flight call failed: %v", err)
		}
		if len(listRes.Tools) != 2 {
			t.Errorf("expected 2 tools, got %d", len(listRes.Tools))
		}
		close(listDone)
	}()

	select {
	case <-listDone:
		// List completed immediately!
	case <-time.After(1 * time.Second):
		t.Fatal("FAIL: ListTools was blocked by in-flight tool call!")
	}

	// Unblock slow_tool and clean up
	close(toolBlockCh)
	<-callDone
}

func TestPublisherConcurrencyRace(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	server := mcp.NewServer(serverImpl("test-hub"), nil)
	routerCb := func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "race_ok"}},
		}, nil
	}

	pub, err := NewPublisher(server, nil, routerCb)
	if err != nil {
		t.Fatalf("NewPublisher: %v", err)
	}

	ct, st := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	client := mcp.NewClient(clientImpl("test-client"), nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	// Initial publish
	_, err = pub.PublishServer("s", []*mcp.Tool{makeValidTool("t0"), makeValidTool("t1")}, nil)
	if err != nil {
		t.Fatalf("initial publish: %v", err)
	}

	var wg sync.WaitGroup
	workers := 4
	iterations := 25

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				switch i % 3 {
				case 0:
					// List tools
					_, _ = cs.ListTools(ctx, nil)
				case 1:
					// Call tool
					_, _ = cs.CallTool(ctx, &mcp.CallToolParams{Name: "s__t0"})
				case 2:
					// Lookup route via pub
					pub.RLock()
					_, _ = pub.LookupRoute("s__t0")
					pub.RUnlock()
				}
			}
		}(w)
	}

	// Publisher updater running concurrently
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			toolName := fmt.Sprintf("dynamic_%d", i%5)
			tools := []*mcp.Tool{
				makeValidTool("t0"),
				makeValidTool(toolName),
			}
			_, _ = pub.PublishServer("s", tools, nil)
		}
	}()

	wg.Wait()
}
