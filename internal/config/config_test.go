package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const validYAML = `chain:
  private_key: "0000000000000000000000000000000000000000000000000000000000000001"
  contract_address: "0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c"
  rpc_url: ""
  broker_chain_url: "https://dash.broker-chain.com:443/"
  use_broker_chain: true
server:
  http_listen: ":8081"
ipfs:
  gateway: "http://127.0.0.1:8080/ipfs"
oracle:
  gold_api_url: "https://api.gold-api.com/price/XAU"
  sina_url: "https://hq.sinajs.cn/list=hf_XAU"
  sina_referer: "https://finance.sina.com.cn"
  user_agent: "PredictionMarket/1.0"
  request_timeout_seconds: 10
sentinel:
  poll_interval_seconds: 30
  resolve_delay_seconds: 5
ai:
  api_key: "test-ai-key"
  base_url: "https://api.deepseek.com/chat/completions"
  model: "deepseek-chat"
  poll_interval_seconds: 120
  buy_amount_bkc: "10"
  confidence_min: 0.70
`

func writeTestConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadFileReadsCompleteYAML(t *testing.T) {
	cfg, err := LoadFile(writeTestConfig(t, validYAML))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AIAPIKey != "test-ai-key" || cfg.AIModel != "deepseek-chat" {
		t.Fatalf("unexpected AI config: %+v", cfg)
	}
	if cfg.IPFSGateway != "http://127.0.0.1:8080/ipfs/" {
		t.Fatalf("IPFS gateway was not normalized: %q", cfg.IPFSGateway)
	}
	if cfg.GoldAPIURL != "https://api.gold-api.com/price/XAU" ||
		cfg.SinaURL != "https://hq.sinajs.cn/list=hf_XAU" ||
		cfg.OracleRequestTimeout != 10*time.Second {
		t.Fatalf("unexpected oracle config: %+v", cfg)
	}
	if cfg.PollInterval != 30*time.Second || cfg.AIPollInterval != 120*time.Second {
		t.Fatalf("unexpected intervals: poll=%s ai=%s", cfg.PollInterval, cfg.AIPollInterval)
	}
}

func TestLoadFileRejectsInvalidConfiguration(t *testing.T) {
	tests := map[string]string{
		"wallet key": strings.Replace(validYAML,
			"0000000000000000000000000000000000000000000000000000000000000001",
			"replace-with-wallet-private-key", 1),
		"AI key": strings.Replace(validYAML, "test-ai-key", "replace-with-ai-api-key", 1),
		"contract": strings.Replace(validYAML,
			"0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c", "not-an-address", 1),
		"RPC mode without URL": strings.Replace(validYAML,
			"use_broker_chain: true", "use_broker_chain: false", 1),
		"AI URL": strings.Replace(validYAML,
			"https://api.deepseek.com/chat/completions", "ftp://invalid", 1),
		"oracle URL": strings.Replace(validYAML,
			"https://api.gold-api.com/price/XAU", "ftp://invalid", 1),
		"poll interval": strings.Replace(validYAML,
			"poll_interval_seconds: 30", "poll_interval_seconds: 0", 1),
		"confidence": strings.Replace(validYAML,
			"confidence_min: 0.70", "confidence_min: 1.1", 1),
		"buy amount": strings.Replace(validYAML,
			`buy_amount_bkc: "10"`, `buy_amount_bkc: "0"`, 1),
		"unknown field": validYAML + "unexpected: true\n",
	}

	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadFile(writeTestConfig(t, body)); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestLoadFileReportsMissingFile(t *testing.T) {
	_, err := LoadFile(filepath.Join(t.TempDir(), "missing.yaml"))
	if err == nil || !strings.Contains(err.Error(), "read config") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRepositoryConfigurationArtifactsUseYAML(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	example, err := os.ReadFile(filepath.Join(root, "config.example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	usable := strings.ReplaceAll(string(example),
		"replace-with-wallet-private-key",
		"0000000000000000000000000000000000000000000000000000000000000001")
	usable = strings.ReplaceAll(usable, "replace-with-ai-api-key", "test-ai-key")
	if _, err := LoadFile(writeTestConfig(t, usable)); err != nil {
		t.Fatalf("example config is not valid after inserting secrets: %v", err)
	}

	for _, name := range []string{"start.sh", "SETUP.md"} {
		body, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), "config.example.xml") ||
			strings.Contains(string(body), "读取 config.xml") {
			t.Fatalf("%s still instructs users to configure XML", name)
		}
		if !strings.Contains(string(body), "config.yaml") {
			t.Fatalf("%s does not mention config.yaml", name)
		}
	}
}
