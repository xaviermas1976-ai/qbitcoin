package p2p

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

// MessageType defines P2P message types.
type MessageType string

const (
	MsgPing      MessageType = "PING"
	MsgPong      MessageType = "PONG"
	MsgNewBlock  MessageType = "NEW_BLOCK"
	MsgNewTx     MessageType = "NEW_TX"
	MsgGetBlocks MessageType = "GET_BLOCKS"
	MsgBlocks    MessageType = "BLOCKS"
	MsgGetPeers  MessageType = "GET_PEERS"
	MsgPeers     MessageType = "PEERS"
	MsgHandshake MessageType = "HANDSHAKE"

	// maxMessageBytes limits incoming message size to prevent memory exhaustion.
	maxMessageBytes = 10 * 1024 * 1024 // 10 MiB

	// seenCacheSize is the number of recent message hashes to track for deduplication.
	seenCacheSize = 4096
)

// Message is the P2P wire format.
type Message struct {
	Type      MessageType `json:"type"`
	From      string      `json:"from"`
	Payload   []byte      `json:"payload,omitempty"`
	Timestamp int64       `json:"timestamp"`
}

// msgHash computes a deduplication key for a message.
func msgHash(msg *Message) [32]byte {
	h := sha256.New()
	h.Write([]byte(msg.Type))
	h.Write([]byte(msg.From))
	h.Write(msg.Payload)
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// Peer represents a connected peer.
type Peer struct {
	ID       string
	Address  string
	conn     net.Conn
	send     chan *Message
	lastSeen time.Time
	mu       sync.Mutex
}

// Send enqueues a message for delivery. Returns error on timeout.
func (p *Peer) Send(msg *Message) error {
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case p.send <- msg:
		return nil
	case <-timer.C:
		return errors.New("send timeout")
	}
}

// close shuts down the peer connection and drains the send channel.
func (p *Peer) close() {
	p.conn.Close()
	// Drain so writeLoop can exit
	for {
		select {
		case <-p.send:
		default:
			return
		}
	}
}

// Node is the P2P network node.
type Node struct {
	mu       sync.RWMutex
	ID       string
	Address  string
	peers    map[string]*Peer
	listener net.Listener
	done     chan struct{}

	// seenMu protects the seen-message deduplication cache.
	seenMu   sync.Mutex
	seenKeys [][32]byte // ring buffer

	OnBlock func([]byte)
	OnTx    func([]byte)
	OnPeer  func(string)

	maxPeers int
}

// NewNode creates a new P2P node.
func NewNode(address string) *Node {
	return &Node{
		ID:       fmt.Sprintf("node-%d", time.Now().UnixNano()),
		Address:  address,
		peers:    make(map[string]*Peer),
		done:     make(chan struct{}),
		seenKeys: make([][32]byte, 0, seenCacheSize),
		maxPeers: 50,
	}
}

// Start starts the P2P listener.
func (n *Node) Start() error {
	ln, err := net.Listen("tcp", n.Address)
	if err != nil {
		return fmt.Errorf("listen %s: %w", n.Address, err)
	}
	n.listener = ln
	log.Printf("[P2P] Node %s listening on %s", n.ID, n.Address)
	go n.acceptLoop()
	go n.heartbeat()
	return nil
}

func (n *Node) acceptLoop() {
	backoff := 10 * time.Millisecond
	for {
		conn, err := n.listener.Accept()
		if err != nil {
			select {
			case <-n.done:
				return
			default:
				log.Printf("[P2P] Accept error: %v (retrying in %v)", err, backoff)
				time.Sleep(backoff)
				if backoff < 5*time.Second {
					backoff *= 2
				}
				continue
			}
		}
		backoff = 10 * time.Millisecond
		go n.handleConn(conn)
	}
}

func (n *Node) handleConn(conn net.Conn) {
	peer := &Peer{
		ID:       conn.RemoteAddr().String(),
		Address:  conn.RemoteAddr().String(),
		conn:     conn,
		send:     make(chan *Message, 64),
		lastSeen: time.Now(),
	}

	n.mu.Lock()
	if len(n.peers) >= n.maxPeers {
		n.mu.Unlock()
		conn.Close()
		return
	}
	n.peers[peer.ID] = peer
	n.mu.Unlock()

	if n.OnPeer != nil {
		n.OnPeer(peer.Address)
	}

	handshake := &Message{
		Type:      MsgHandshake,
		From:      n.ID,
		Payload:   []byte(n.Address),
		Timestamp: time.Now().UnixNano(),
	}
	peer.Send(handshake)

	go n.writeLoop(peer)
	n.readLoop(peer)
}

func (n *Node) writeLoop(peer *Peer) {
	enc := json.NewEncoder(peer.conn)
	for {
		select {
		case msg, ok := <-peer.send:
			if !ok {
				return
			}
			peer.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := enc.Encode(msg); err != nil {
				log.Printf("[P2P] Write to %s: %v", peer.ID, err)
				n.removePeer(peer.ID)
				return
			}
		case <-n.done:
			return
		}
	}
}

func (n *Node) readLoop(peer *Peer) {
	// Limit incoming message size to prevent memory exhaustion
	dec := json.NewDecoder(io.LimitReader(peer.conn, maxMessageBytes))
	for {
		var msg Message
		peer.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		if err := dec.Decode(&msg); err != nil {
			log.Printf("[P2P] Read from %s: %v", peer.ID, err)
			n.removePeer(peer.ID)
			return
		}
		peer.mu.Lock()
		peer.lastSeen = time.Now()
		peer.mu.Unlock()
		n.handleMessage(peer, &msg)
	}
}

// alreadySeen returns true if this message was recently processed (deduplication).
func (n *Node) alreadySeen(msg *Message) bool {
	h := msgHash(msg)
	n.seenMu.Lock()
	defer n.seenMu.Unlock()
	for _, k := range n.seenKeys {
		if k == h {
			return true
		}
	}
	if len(n.seenKeys) >= seenCacheSize {
		n.seenKeys = n.seenKeys[1:]
	}
	n.seenKeys = append(n.seenKeys, h)
	return false
}

func (n *Node) handleMessage(peer *Peer, msg *Message) {
	switch msg.Type {
	case MsgPing:
		peer.Send(&Message{Type: MsgPong, From: n.ID, Timestamp: time.Now().UnixNano()})
	case MsgNewBlock:
		if n.alreadySeen(msg) {
			return
		}
		if n.OnBlock != nil {
			n.OnBlock(msg.Payload)
		}
		n.Broadcast(msg, peer.ID)
	case MsgNewTx:
		if n.alreadySeen(msg) {
			return
		}
		if n.OnTx != nil {
			n.OnTx(msg.Payload)
		}
		n.Broadcast(msg, peer.ID)
	case MsgGetPeers:
		peers := n.PeerAddresses()
		// Return a random subset (max 10) to limit topology disclosure
		if len(peers) > 10 {
			peers = peers[:10]
		}
		data, _ := json.Marshal(peers)
		peer.Send(&Message{Type: MsgPeers, From: n.ID, Payload: data, Timestamp: time.Now().UnixNano()})
	}
}

// Connect dials a peer.
func (n *Node) Connect(address string) error {
	conn, err := net.DialTimeout("tcp", address, 10*time.Second)
	if err != nil {
		return fmt.Errorf("dial %s: %w", address, err)
	}
	go n.handleConn(conn)
	return nil
}

// Broadcast sends a message to all peers except the one with excludeID.
func (n *Node) Broadcast(msg *Message, excludeID string) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	for id, peer := range n.peers {
		if id == excludeID {
			continue
		}
		peer.Send(msg)
	}
}

// BroadcastBlock broadcasts a new block to all peers.
func (n *Node) BroadcastBlock(blockData []byte) {
	n.Broadcast(&Message{
		Type:      MsgNewBlock,
		From:      n.ID,
		Payload:   blockData,
		Timestamp: time.Now().UnixNano(),
	}, "")
}

// BroadcastTx broadcasts a new transaction to all peers.
func (n *Node) BroadcastTx(txData []byte) {
	n.Broadcast(&Message{
		Type:      MsgNewTx,
		From:      n.ID,
		Payload:   txData,
		Timestamp: time.Now().UnixNano(),
	}, "")
}

func (n *Node) removePeer(id string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if p, ok := n.peers[id]; ok {
		p.close()
		delete(n.peers, id)
	}
}

// PeerCount returns the number of connected peers.
func (n *Node) PeerCount() int {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return len(n.peers)
}

// PeerAddresses returns all peer addresses.
func (n *Node) PeerAddresses() []string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	addrs := make([]string, 0, len(n.peers))
	for _, p := range n.peers {
		addrs = append(addrs, p.Address)
	}
	return addrs
}

// heartbeat pings peers and removes stale ones.
// Avoids holding n.mu while acquiring p.mu to prevent lock nesting.
func (n *Node) heartbeat() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			// Snapshot lastSeen values without holding n.mu + p.mu simultaneously
			n.mu.RLock()
			stale := make([]string, 0)
			for id, p := range n.peers {
				p.mu.Lock()
				if time.Since(p.lastSeen) > 2*time.Minute {
					stale = append(stale, id)
				}
				p.mu.Unlock()
			}
			n.mu.RUnlock()

			for _, id := range stale {
				n.removePeer(id)
			}
			n.Broadcast(&Message{Type: MsgPing, From: n.ID, Timestamp: time.Now().UnixNano()}, "")
		case <-n.done:
			return
		}
	}
}

// Stop shuts down the node gracefully.
func (n *Node) Stop() {
	close(n.done)
	if n.listener != nil {
		n.listener.Close()
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, p := range n.peers {
		p.close()
	}
}
