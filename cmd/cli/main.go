package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

var apiBase string

func main() {
	api := flag.String("api", "http://127.0.0.1:8080", "API server URL")
	flag.Parse()
	apiBase = *api

	fmt.Println("qBitcoin CLI — type 'help' for commands")
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("qbtc> ")
		if !scanner.Scan() {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if err := run(parts); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		}
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return nil
	}
	switch args[0] {
	case "help":
		printHelp()
	case "status":
		return cmdStatus()
	case "blocks":
		return cmdBlocks(args[1:])
	case "block":
		if len(args) < 2 {
			return fmt.Errorf("usage: block <index>")
		}
		return cmdBlock(args[1])
	case "wallet":
		return cmdWallet(args[1:])
	case "send":
		return cmdSend(args[1:])
	case "mempool":
		return cmdMempool()
	case "validator":
		return cmdValidator(args[1:])
	case "exit", "quit":
		fmt.Println("Bye!")
		os.Exit(0)
	default:
		return fmt.Errorf("unknown command: %s (type 'help')", args[0])
	}
	return nil
}

func printHelp() {
	fmt.Print(`Commands:
  status                  — node status
  blocks [limit] [offset] — list blocks
  block <index>           — show block details
  wallet new [label]      — create new wallet
  wallet list             — list wallets
  wallet <address>        — show wallet info
  send <from> <to> <amt>  — send transaction
  mempool                 — mempool info
  validator register <addr> <stake> — register validator
  exit                    — quit
`)
}

func get(path string, result interface{}) error {
	resp, err := http.Get(apiBase + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("API error %d: %s", resp.StatusCode, body)
	}
	return json.Unmarshal(body, result)
}

func post(path string, payload, result interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	resp, err := http.Post(apiBase+path, "application/json", strings.NewReader(string(data)))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("API error %d: %s", resp.StatusCode, body)
	}
	return json.Unmarshal(body, result)
}

func prettyPrint(v interface{}) {
	data, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(data))
}

func cmdStatus() error {
	var result map[string]interface{}
	if err := get("/api/v1/status", &result); err != nil {
		return err
	}
	prettyPrint(result)
	return nil
}

func cmdBlocks(args []string) error {
	limit := "10"
	offset := "0"
	if len(args) > 0 {
		limit = args[0]
	}
	if len(args) > 1 {
		offset = args[1]
	}
	var result interface{}
	if err := get(fmt.Sprintf("/api/v1/blocks?limit=%s&offset=%s", limit, offset), &result); err != nil {
		return err
	}
	prettyPrint(result)
	return nil
}

func cmdBlock(index string) error {
	var result interface{}
	if err := get("/api/v1/block/"+index, &result); err != nil {
		return err
	}
	prettyPrint(result)
	return nil
}

func cmdWallet(args []string) error {
	if len(args) == 0 {
		fmt.Println("usage: wallet new [label] | wallet list | wallet <address>")
		return nil
	}
	switch args[0] {
	case "new":
		label := ""
		if len(args) > 1 {
			label = strings.Join(args[1:], " ")
		}
		var result interface{}
		if err := post("/api/v1/wallet/new", map[string]string{"label": label}, &result); err != nil {
			return err
		}
		prettyPrint(result)
	case "list":
		var result interface{}
		if err := get("/api/v1/wallet/", &result); err != nil {
			return err
		}
		prettyPrint(result)
	default:
		var result interface{}
		if err := get("/api/v1/wallet/"+args[0], &result); err != nil {
			return err
		}
		prettyPrint(result)
	}
	return nil
}

func cmdSend(args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("usage: send <from> <to> <amount>")
	}
	var amount uint64
	fmt.Sscanf(args[2], "%d", &amount)
	payload := map[string]interface{}{
		"type":   "TRANSFER",
		"from":   args[0],
		"to":     args[1],
		"amount": amount,
		"fee":    1000,
	}
	var result interface{}
	if err := post("/api/v1/tx", payload, &result); err != nil {
		return err
	}
	prettyPrint(result)
	return nil
}

func cmdMempool() error {
	var result interface{}
	if err := get("/api/v1/mempool", &result); err != nil {
		return err
	}
	prettyPrint(result)
	return nil
}

func cmdValidator(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: validator register <address> <stake>")
	}
	if args[0] == "register" {
		if len(args) < 3 {
			return fmt.Errorf("usage: validator register <address> <stake>")
		}
		var stake uint64
		fmt.Sscanf(args[2], "%d", &stake)
		payload := map[string]interface{}{
			"address":    args[1],
			"public_key": []byte{},
			"stake":      stake,
		}
		var result interface{}
		if err := post("/api/v1/validator/register", payload, &result); err != nil {
			return err
		}
		prettyPrint(result)
	}
	return nil
}
