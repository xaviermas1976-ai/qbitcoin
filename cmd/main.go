package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"qbitcoin/internal/api"
	"qbitcoin/internal/blockchain"
	"qbitcoin/internal/contracts"
	"qbitcoin/internal/p2p"
	"qbitcoin/internal/wallet"
)

func main() {
	var (
		apiAddr = flag.String("api", "127.0.0.1:8080", "REST API listen address (default: localhost only)")
		p2pAddr = flag.String("p2p", ":9090", "P2P listen address")
		peers   = flag.String("peers", "", "Comma-separated bootstrap peers")
		dataDir = flag.String("data", "./data", "Data directory")
	)
	flag.Parse()

	if err := os.MkdirAll(*dataDir, 0755); err != nil {
		log.Fatalf("[qBitcoin] Cannot create data dir %s: %v", *dataDir, err)
	}

	log.Println("[qBitcoin] Starting node...")

	bc := blockchain.NewBlockchain()
	log.Printf("[qBitcoin] Blockchain initialized, height: %d", bc.Height())

	wm := wallet.NewManager()
	vm := contracts.NewVM()

	node := p2p.NewNode(*p2pAddr)
	node.OnBlock = func(data []byte) {
		log.Printf("[P2P] Received new block (%d bytes)", len(data))
	}
	node.OnTx = func(data []byte) {
		var tx blockchain.Transaction
		if err := json.Unmarshal(data, &tx); err == nil {
			if err := bc.AddTransaction(&tx); err != nil {
				log.Printf("[P2P] Rejected tx: %v", err)
			}
		}
	}
	node.OnPeer = func(addr string) {
		log.Printf("[P2P] New peer: %s", addr)
	}

	if err := node.Start(); err != nil {
		log.Fatalf("[P2P] Failed to start: %v", err)
	}

	if *peers != "" {
		for _, addr := range strings.Split(*peers, ",") {
			addr = strings.TrimSpace(addr)
			if addr == "" {
				continue
			}
			go func(a string) {
				if err := node.Connect(a); err != nil {
					log.Printf("[P2P] Cannot connect to %s: %v", a, err)
				}
			}(addr)
		}
	}

	server := api.NewServer(*apiAddr, bc, wm, vm)

	go blockProducer(bc, node)

	// Run API server; send errors to channel so main goroutine handles them.
	apiErr := make(chan error, 1)
	go func() {
		if err := server.Start(); err != nil {
			apiErr <- err
		}
	}()

	log.Printf("[qBitcoin] Node running — API: %s | P2P: %s", *apiAddr, *p2pAddr)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	select {
	case <-sig:
		log.Println("[qBitcoin] Shutdown signal received...")
	case err := <-apiErr:
		log.Printf("[qBitcoin] API server error: %v", err)
	}

	// Graceful shutdown
	node.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("[qBitcoin] API shutdown error: %v", err)
	}
	log.Println("[qBitcoin] Stopped.")
}

// blockProducer runs a PoS block production loop.
func blockProducer(bc *blockchain.Blockchain, node *p2p.Node) {
	ticker := time.NewTicker(bc.BlockTime)
	defer ticker.Stop()
	for range ticker.C {
		validator := bc.SelectValidator()
		if validator == nil {
			continue
		}
		block, err := bc.MintBlock(validator)
		if err != nil {
			log.Printf("[PoS] Mint error: %v", err)
			continue
		}
		log.Printf("[PoS] Block #%d minted by %s (%d txs)",
			block.Index, block.Validator, len(block.Transactions))
		data, err := json.Marshal(block)
		if err != nil {
			log.Printf("[PoS] Marshal block error: %v", err)
			continue
		}
		node.BroadcastBlock(data)
	}
}
