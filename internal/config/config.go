package config

import (
	"encoding/xml"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

const DefaultContractAddress = "0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c"
const ConfigFileName = "config.xml"

// XMLConfig 对应 XML 文件结构
type XMLConfig struct {
	XMLName         xml.Name `xml:"config"`
	PrivateKey      string   `xml:"private_key"`
	ContractAddress string   `xml:"contract_address"`
	RPCURL          string   `xml:"rpc_url"`
	BrokerChainURL  string   `xml:"broker_chain_url"`
	IPFSGateway     string   `xml:"ipfs_gateway"`
	PollInterval    string   `xml:"poll_interval"`
	ResolveDelay    string   `xml:"resolve_delay"`
	UseBrokerChain  string   `xml:"use_broker_chain"`
}

type Config struct {
	PrivateKey       string
	ContractAddress  string
	RPCURL           string
	BrokerChainURL   string
	IPFSGateway      string
	PollInterval     time.Duration
	ResolveDelay     time.Duration
	UseBrokerChain   bool
}

func Load() (*Config, error) {
	// 尝试加载 XML 配置文件
	if _, err := os.Stat(ConfigFileName); err == nil {
		slog.Info("loading config from XML file", "file", ConfigFileName)
		return loadFromXML()
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("error checking config file: %w", err)
	}

	slog.Info("XML config file not found", "file", ConfigFileName)
	return nil, fmt.Errorf("config file %s not found: please copy config.example.xml to %s and configure it", ConfigFileName, ConfigFileName)
}

func loadFromXML() (*Config, error) {
	data, err := os.ReadFile(ConfigFileName)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var xmlCfg XMLConfig
	if err := xml.Unmarshal(data, &xmlCfg); err != nil {
		return nil, fmt.Errorf("failed to parse XML config: %w", err)
	}

	// 验证必需配置
	privateKey := strings.TrimSpace(xmlCfg.PrivateKey)
	if privateKey == "" || privateKey == "your_private_key_here" {
		return nil, fmt.Errorf("private_key is required: please set it in %s", ConfigFileName)
	}

	// 处理配置项
	contract := strings.TrimSpace(xmlCfg.ContractAddress)
	if contract == "" {
		contract = DefaultContractAddress
	}

	rpcURL := strings.TrimSpace(xmlCfg.RPCURL)
	brokerChainURL := strings.TrimSpace(xmlCfg.BrokerChainURL)
	if brokerChainURL == "" {
		brokerChainURL = "https://dash.broker-chain.com:443/"
	}

	ipfsGateway := strings.TrimSpace(xmlCfg.IPFSGateway)
	if ipfsGateway == "" {
		ipfsGateway = "http://127.0.0.1:8080/ipfs/"
	}
	if !strings.HasSuffix(ipfsGateway, "/") {
		ipfsGateway += "/"
	}

	// 解析 PollInterval
	pollInterval := 30 * time.Second
	if v := strings.TrimSpace(xmlCfg.PollInterval); v != "" {
		sec, err := strconv.Atoi(v)
		if err != nil || sec <= 0 {
			return nil, fmt.Errorf("invalid poll_interval: %q", v)
		}
		pollInterval = time.Duration(sec) * time.Second
	}

	// 解析 ResolveDelay
	resolveDelay := 5 * time.Second
	if v := strings.TrimSpace(xmlCfg.ResolveDelay); v != "" {
		sec, err := strconv.Atoi(v)
		if err != nil || sec < 0 {
			return nil, fmt.Errorf("invalid resolve_delay: %q", v)
		}
		resolveDelay = time.Duration(sec) * time.Second
	}

	// 解析 UseBrokerChain
	useBrokerChain := rpcURL == ""
	if v := strings.TrimSpace(xmlCfg.UseBrokerChain); v != "" {
		useBrokerChain = strings.EqualFold(v, "true") || v == "1"
	}

	return &Config{
		PrivateKey:      privateKey,
		ContractAddress: contract,
		RPCURL:          rpcURL,
		BrokerChainURL:  brokerChainURL,
		IPFSGateway:     ipfsGateway,
		PollInterval:    pollInterval,
		ResolveDelay:    resolveDelay,
		UseBrokerChain:  useBrokerChain,
	}, nil
}
