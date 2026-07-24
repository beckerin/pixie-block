package p2p

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

type MessageType string

const (
	MsgHandshake      MessageType = "handshake"
	MsgNewTransaction MessageType = "new_transaction"
	MsgNewBlock       MessageType = "new_block"
	MsgGetBlocks      MessageType = "get_blocks"
	MsgBlocksResponse MessageType = "blocks_response"
)

type Message struct {
	Type    MessageType     `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type Handshake struct {
	ChainID string `json:"chain_id"`
	NodeID  string `json:"node_id"`
	Height  int64  `json:"height"`
}

type GetBlocks struct {
	FromHeight int64 `json:"from_height"`
}

type Handler interface {
	OnNewTransaction(data json.RawMessage) error
	OnNewBlock(data json.RawMessage) error
	OnGetBlocks(fromHeight int64) (json.RawMessage, error)
	CurrentHeight() int64
	ChainID() string
	NodeID() string
}

type Node struct {
	listenAddr string
	peers      []string
	handler    Handler

	mu      sync.Mutex
	conns   map[string]net.Conn
	encoder map[string]*json.Encoder
}

func New(listenAddr string, peers []string, handler Handler) *Node {
	return &Node{
		listenAddr: listenAddr,
		peers:      peers,
		handler:    handler,
		conns:      make(map[string]net.Conn),
		encoder:    make(map[string]*json.Encoder),
	}
}

func (n *Node) Start() error {
	ln, err := net.Listen("tcp", n.listenAddr)
	if err != nil {
		return fmt.Errorf("p2p listen: %w", err)
	}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go n.handleConn(conn, true)
		}
	}()

	for _, peer := range n.peers {
		go n.dial(peer)
	}

	return nil
}

func (n *Node) dial(addr string) {
	for {
		conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
		if err != nil {
			time.Sleep(3 * time.Second)
			continue
		}
		n.handleConn(conn, false)
		return
	}
}

func (n *Node) registerConn(addr string, conn net.Conn) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.conns[addr] = conn
	n.encoder[addr] = json.NewEncoder(conn)
}

func (n *Node) unregisterConn(addr string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.conns, addr)
	delete(n.encoder, addr)
}

func (n *Node) handleConn(conn net.Conn, inbound bool) {
	addr := conn.RemoteAddr().String()
	n.registerConn(addr, conn)
	defer func() {
		n.unregisterConn(addr)
		_ = conn.Close()
	}()

	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	if !inbound {
		if err := n.sendHandshake(encoder); err != nil {
			return
		}
	}

	for {
		var msg Message
		if err := decoder.Decode(&msg); err != nil {
			if err != io.EOF {
				return
			}
			return
		}

		if err := n.dispatch(msg, encoder); err != nil {
			return
		}
	}
}

func (n *Node) sendHandshake(encoder *json.Encoder) error {
	payload, err := json.Marshal(Handshake{
		ChainID: n.handler.ChainID(),
		NodeID:  n.handler.NodeID(),
		Height:  n.handler.CurrentHeight(),
	})
	if err != nil {
		return err
	}
	return encoder.Encode(Message{Type: MsgHandshake, Payload: payload})
}

func (n *Node) dispatch(msg Message, encoder *json.Encoder) error {
	switch msg.Type {
	case MsgHandshake:
		var hs Handshake
		if err := json.Unmarshal(msg.Payload, &hs); err != nil {
			return err
		}
		if hs.ChainID != n.handler.ChainID() {
			return fmt.Errorf("chain id mismatch")
		}
		if hs.Height > n.handler.CurrentHeight() {
			return n.requestSync(encoder, n.handler.CurrentHeight()+1)
		}
		return nil
	case MsgNewTransaction:
		return n.handler.OnNewTransaction(msg.Payload)
	case MsgNewBlock:
		return n.handler.OnNewBlock(msg.Payload)
	case MsgGetBlocks:
		var req GetBlocks
		if err := json.Unmarshal(msg.Payload, &req); err != nil {
			return err
		}
		resp, err := n.handler.OnGetBlocks(req.FromHeight)
		if err != nil {
			return err
		}
		return encoder.Encode(Message{Type: MsgBlocksResponse, Payload: resp})
	case MsgBlocksResponse:
		return n.handler.OnNewBlock(msg.Payload)
	default:
		return nil
	}
}

func (n *Node) requestSync(encoder *json.Encoder, fromHeight int64) error {
	payload, err := json.Marshal(GetBlocks{FromHeight: fromHeight})
	if err != nil {
		return err
	}
	return encoder.Encode(Message{Type: MsgGetBlocks, Payload: payload})
}

func (n *Node) Broadcast(msgType MessageType, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	msg := Message{Type: msgType, Payload: data}
	for _, enc := range n.encoder {
		_ = enc.Encode(msg)
	}
}
