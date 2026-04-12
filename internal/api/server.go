package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"qbitcoin/internal/blockchain"
	"qbitcoin/internal/contracts"
	"qbitcoin/internal/wallet"

	"github.com/google/uuid"
)

const (
	maxBodyBytes = 1 << 20 // 1 MiB per request
)

// Server is the REST API server.
type Server struct {
	bc      *blockchain.Blockchain
	wallets *wallet.Manager
	vm      *contracts.VM
	hub     *SSEHub
	srv     *http.Server
}

// NewServer creates a new API server.
// Binds to localhost by default; pass "0.0.0.0:port" to expose externally.
func NewServer(addr string, bc *blockchain.Blockchain, wm *wallet.Manager, vm *contracts.VM) *Server {
	s := &Server{
		bc:      bc,
		wallets: wm,
		vm:      vm,
		hub:     NewSSEHub(),
	}

	mux := http.NewServeMux()
	s.routes(mux)

	s.srv = &http.Server{
		Addr:         addr,
		Handler:      s.corsMiddleware(mux),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	return s
}

func (s *Server) routes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/status", s.handleStatus)
	mux.HandleFunc("/api/v1/blocks", s.handleBlocks)
	mux.HandleFunc("/api/v1/block/", s.handleBlock)
	mux.HandleFunc("/api/v1/tx", s.handleSendTx)
	mux.HandleFunc("/api/v1/mempool", s.handleMempool)
	mux.HandleFunc("/api/v1/wallet/new", s.handleNewWallet)
	mux.HandleFunc("/api/v1/wallet/", s.handleWallet)
	mux.HandleFunc("/api/v1/validator/register", s.handleRegisterValidator)
	mux.HandleFunc("/api/v1/contract/qrc20", s.handleDeployQRC20)
	mux.HandleFunc("/api/v1/contract/nft", s.handleDeployNFT)
	mux.HandleFunc("/api/v1/contract/dao", s.handleDeployDAO)
	mux.HandleFunc("/api/v1/events", s.handleSSE)
}

// Start starts the HTTP server. Returns when the server stops.
func (s *Server) Start() error {
	log.Printf("[API] Server listening on %s", s.srv.Addr)
	return s.srv.ListenAndServe()
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.hub.Stop()
	return s.srv.Shutdown(ctx)
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Restrict CORS to same origin or localhost only
		origin := r.Header.Get("Origin")
		if origin == "http://localhost:3000" || origin == "http://127.0.0.1:3000" || origin == "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("[API] JSON encode error: %v", err)
	}
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func limitBody(r *http.Request) {
	r.Body = http.MaxBytesReader(nil, r.Body, maxBodyBytes)
}

// GET /api/v1/status
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, 405, "method not allowed")
		return
	}
	writeJSON(w, 200, map[string]interface{}{
		"height":       s.bc.Height(),
		"mempool_size": s.bc.MempoolSize(),
		"sse_clients":  s.hub.ClientCount(),
		"timestamp":    time.Now().UnixNano(),
	})
}

// GET /api/v1/blocks?limit=10&offset=0
func (s *Server) handleBlocks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, 405, "method not allowed")
		return
	}
	limit, _ := strconv.ParseUint(r.URL.Query().Get("limit"), 10, 64)
	offset, _ := strconv.ParseUint(r.URL.Query().Get("offset"), 10, 64)
	if limit == 0 || limit > 100 {
		limit = 10
	}
	height := s.bc.Height()
	blocks := make([]*blockchain.Block, 0, limit)
	for i := offset; i < offset+limit && i < height; i++ {
		if b, err := s.bc.GetBlock(i); err == nil {
			blocks = append(blocks, b)
		}
	}
	writeJSON(w, 200, blocks)
}

// GET /api/v1/block/{index}
func (s *Server) handleBlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, 405, "method not allowed")
		return
	}
	idxStr := r.URL.Path[len("/api/v1/block/"):]
	idx, err := strconv.ParseUint(idxStr, 10, 64)
	if err != nil {
		writeError(w, 400, "invalid block index")
		return
	}
	block, err := s.bc.GetBlock(idx)
	if err != nil {
		writeError(w, 404, "block not found")
		return
	}
	writeJSON(w, 200, block)
}

// POST /api/v1/tx
func (s *Server) handleSendTx(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	var tx blockchain.Transaction
	if err := json.NewDecoder(r.Body).Decode(&tx); err != nil {
		writeError(w, 400, "invalid JSON: "+err.Error())
		return
	}
	// Only allow user-submitted transfer transactions via the API
	if tx.Type != blockchain.TxTransfer {
		writeError(w, 400, "only TRANSFER transactions accepted via API")
		return
	}
	if tx.From == "" || tx.To == "" {
		writeError(w, 400, "from and to addresses required")
		return
	}
	if err := s.bc.AddTransaction(&tx); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	data, _ := json.Marshal(tx)
	s.hub.Publish("transactions", "new_tx", string(data))
	writeJSON(w, 201, map[string]string{"id": tx.ID, "status": "pending"})
}

// GET /api/v1/mempool
func (s *Server) handleMempool(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, 405, "method not allowed")
		return
	}
	writeJSON(w, 200, map[string]interface{}{
		"size": s.bc.MempoolSize(),
	})
}

// POST /api/v1/wallet/new
func (s *Server) handleNewWallet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	var req struct {
		Label string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON: "+err.Error())
		return
	}
	wlt, err := s.wallets.Create(req.Label)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	info := wlt.ExportPublic()
	writeJSON(w, 201, info)
}

// GET /api/v1/wallet/{address}
func (s *Server) handleWallet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, 405, "method not allowed")
		return
	}
	addr := r.URL.Path[len("/api/v1/wallet/"):]
	if addr == "" {
		writeJSON(w, 200, s.wallets.List())
		return
	}
	bal := s.bc.Balance(addr)
	wlt, err := s.wallets.Get(addr)
	if err != nil {
		writeJSON(w, 200, map[string]interface{}{
			"address": addr,
			"balance": bal,
		})
		return
	}
	info := wlt.ExportPublic()
	writeJSON(w, 200, map[string]interface{}{
		"address":    info.Address,
		"public_key": info.PublicKey,
		"created_at": info.CreatedAt,
		"label":      info.Label,
		"balance":    bal,
	})
}

// POST /api/v1/validator/register
func (s *Server) handleRegisterValidator(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	var req struct {
		Address   string `json:"address"`
		PublicKey []byte `json:"public_key"`
		Stake     uint64 `json:"stake"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if req.Address == "" || req.Stake == 0 {
		writeError(w, 400, "address and stake required")
		return
	}
	if err := s.bc.RegisterValidator(req.Address, req.PublicKey, req.Stake); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, map[string]string{"status": "registered", "address": req.Address})
}

// POST /api/v1/contract/qrc20
func (s *Server) handleDeployQRC20(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	var req struct {
		Owner       string `json:"owner"`
		Name        string `json:"name"`
		Symbol      string `json:"symbol"`
		Decimals    uint8  `json:"decimals"`
		TotalSupply uint64 `json:"total_supply"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if req.Owner == "" || req.Name == "" || req.Symbol == "" || req.TotalSupply == 0 {
		writeError(w, 400, "owner, name, symbol, and total_supply required")
		return
	}
	token := s.vm.DeployQRC20(req.Owner, req.Name, req.Symbol, req.Decimals, req.TotalSupply)
	writeJSON(w, 201, map[string]interface{}{
		"address":      token.Address,
		"name":         token.Name,
		"symbol":       token.Symbol,
		"total_supply": token.TotalSupply(),
	})
}

// POST /api/v1/contract/nft
func (s *Server) handleDeployNFT(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	var req struct {
		Owner  string `json:"owner"`
		Name   string `json:"name"`
		Symbol string `json:"symbol"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if req.Owner == "" || req.Name == "" || req.Symbol == "" {
		writeError(w, 400, "owner, name, and symbol required")
		return
	}
	nft := s.vm.DeployNFT(req.Owner, req.Name, req.Symbol)
	writeJSON(w, 201, map[string]interface{}{
		"address": nft.Address,
		"name":    nft.Name,
		"symbol":  nft.Symbol,
	})
}

// POST /api/v1/contract/dao
func (s *Server) handleDeployDAO(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	var req struct {
		Owner string `json:"owner"`
		Name  string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if req.Owner == "" || req.Name == "" {
		writeError(w, 400, "owner and name required")
		return
	}
	dao := s.vm.DeployDAO(req.Owner, req.Name)
	writeJSON(w, 201, map[string]interface{}{
		"address": dao.Address,
		"name":    dao.Name,
	})
}

// GET /api/v1/events?channel=blocks
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, 500, "SSE not supported")
		return
	}

	channel := r.URL.Query().Get("channel")
	clientID := uuid.New().String()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	client := s.hub.Subscribe(clientID, channel)
	defer s.hub.Unsubscribe(clientID)

	// Send initial connection event using json.Marshal to avoid injection
	connData, _ := json.Marshal(map[string]string{"id": clientID})
	fmt.Fprintf(w, "event: connected\ndata: %s\n\n", connData)
	flusher.Flush()

	for {
		select {
		case msg, ok := <-client.send:
			if !ok {
				return
			}
			fmt.Fprint(w, msg)
			flusher.Flush()
		case <-r.Context().Done():
			return
		case <-client.done:
			return
		}
	}
}
