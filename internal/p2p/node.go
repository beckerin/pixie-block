package p2p

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

type MessageType string

const (
	MsgHandshake        MessageType = "handshake"
	MsgNewTransaction   MessageType = "new_transaction"
	MsgNewAccountCreate MessageType = "new_account_create"
	MsgNewBlock         MessageType = "new_block"
	MsgGetBlocks        MessageType = "get_blocks"
	MsgBlocksResponse   MessageType = "blocks_response"
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
	OnNewAccountCreate(data json.RawMessage) error
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
		log.Printf("p2p connected to %s", addr)
		n.handleConn(conn, false)
		log.Printf("p2p disconnected from %s, reconnecting", addr)
		time.Sleep(3 * time.Second)
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

		if err := n.dispatch(msg, encoder, inbound); err != nil {
			log.Printf("p2p dispatch error from %s: %v", addr, err)
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

func (n *Node) dispatch(msg Message, encoder *json.Encoder, inbound bool) error {
	switch msg.Type {
	case MsgHandshake:
		var hs Handshake
		if err := json.Unmarshal(msg.Payload, &hs); err != nil {
			return err
		}
		if hs.ChainID != n.handler.ChainID() {
			return fmt.Errorf("chain id mismatch")
		}
		// Listener replies so the dialer learns our height and can catch up.
		if inbound {
			if err := n.sendHandshake(encoder); err != nil {
				return err
			}
		}
		if hs.Height > n.handler.CurrentHeight() {
			log.Printf("p2p peer %s ahead (remote=%d local=%d), requesting sync", hs.NodeID, hs.Height, n.handler.CurrentHeight())
			return n.requestSync(encoder, n.handler.CurrentHeight()+1)
		}
		return nil
	case MsgNewTransaction:
		// Gossip validation failures must not drop the peer.
		if err := n.handler.OnNewTransaction(msg.Payload); err != nil {
			log.Printf("p2p ignore transaction: %v", err)
		}
		return nil
	case MsgNewAccountCreate:
		if err := n.handler.OnNewAccountCreate(msg.Payload); err != nil {
			log.Printf("p2p ignore account create: %v", err)
		}
		return nil
	case MsgNewBlock:
		if err := n.handler.OnNewBlock(msg.Payload); err != nil {
			if errors.Is(err, ErrNeedSync) {
				log.Printf("p2p out-of-order block, requesting sync from height %d", n.handler.CurrentHeight()+1)
				return n.requestSync(encoder, n.handler.CurrentHeight()+1)
			}
			log.Printf("p2p ignore block: %v", err)
		}
		return nil
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
		if err := n.handler.OnNewBlock(msg.Payload); err != nil {
			log.Printf("p2p sync apply error: %v", err)
		}
		return nil
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
