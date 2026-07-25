package p2p

import (
	"encoding/json"
	"net"
	"sync"
	"testing"
	"time"
)

type mockHandler struct {
	mu            sync.Mutex
	chainID       string
	nodeID        string
	height        int64
	blocks        []json.RawMessage
	gotBlocksFrom int64
	synced        chan struct{}
	once          sync.Once
}

func (m *mockHandler) ChainID() string { return m.chainID }
func (m *mockHandler) NodeID() string  { return m.nodeID }

func (m *mockHandler) CurrentHeight() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.height
}

func (m *mockHandler) OnNewTransaction(json.RawMessage) error { return nil }

func (m *mockHandler) OnNewAccountCreate(json.RawMessage) error { return nil }

func (m *mockHandler) OnNewAccountClose(json.RawMessage) error { return nil }

func (m *mockHandler) OnNewBlock(data json.RawMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var resp struct {
		Blocks []map[string]any `json:"blocks"`
	}
	if err := json.Unmarshal(data, &resp); err == nil && resp.Blocks != nil {
		m.height += int64(len(resp.Blocks))
		m.once.Do(func() { close(m.synced) })
		return nil
	}
	m.height++
	m.once.Do(func() { close(m.synced) })
	return nil
}

func (m *mockHandler) OnGetBlocks(fromHeight int64) (json.RawMessage, error) {
	m.mu.Lock()
	m.gotBlocksFrom = fromHeight
	m.mu.Unlock()
	payload, _ := json.Marshal(struct {
		Blocks []map[string]any `json:"blocks"`
	}{
		Blocks: []map[string]any{
			{"height": fromHeight},
			{"height": fromHeight + 1},
		},
	})
	return payload, nil
}

func TestHandshakeTriggersCatchUpSync(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	ahead := &mockHandler{chainID: "test", nodeID: "ahead", height: 10}
	behind := &mockHandler{
		chainID: "test",
		nodeID:  "behind",
		height:  0,
		synced:  make(chan struct{}),
	}

	aheadNode := New(ln.Addr().String(), nil, ahead)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go aheadNode.handleConn(conn, true)
		}
	}()

	behindNode := New("127.0.0.1:0", []string{ln.Addr().String()}, behind)
	go behindNode.dial(ln.Addr().String())

	select {
	case <-behind.synced:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for catch-up sync")
	}

	if got := behind.CurrentHeight(); got < 2 {
		t.Fatalf("behind height = %d, want at least 2 after sync", got)
	}
	ahead.mu.Lock()
	from := ahead.gotBlocksFrom
	ahead.mu.Unlock()
	if from != 1 {
		t.Fatalf("get_blocks from_height = %d, want 1", from)
	}
}

func TestInboundRepliesWithHandshake(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	h := &mockHandler{chainID: "test", nodeID: "server", height: 5}
	n := New("", nil, h)

	done := make(chan struct{})
	go func() {
		n.handleConn(server, true)
		close(done)
	}()

	enc := json.NewEncoder(client)
	dec := json.NewDecoder(client)

	payload, _ := json.Marshal(Handshake{ChainID: "test", NodeID: "client", Height: 0})
	if err := enc.Encode(Message{Type: MsgHandshake, Payload: payload}); err != nil {
		t.Fatal(err)
	}

	var reply Message
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err := dec.Decode(&reply); err != nil {
		t.Fatalf("expected handshake reply: %v", err)
	}
	if reply.Type != MsgHandshake {
		t.Fatalf("reply type = %s, want handshake", reply.Type)
	}
	var hs Handshake
	if err := json.Unmarshal(reply.Payload, &hs); err != nil {
		t.Fatal(err)
	}
	if hs.Height != 5 || hs.NodeID != "server" {
		t.Fatalf("unexpected handshake reply: %+v", hs)
	}

	_ = client.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handleConn did not exit")
	}
}
