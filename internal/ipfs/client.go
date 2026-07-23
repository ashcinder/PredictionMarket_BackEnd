package ipfs

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Metadata struct {
	Type                 string          `json:"type,omitempty"`
	Desc                 string          `json:"desc"`
	Condition            string          `json:"condition"`
	AvatarURL            string          `json:"avatarUrl"`
	DetailedInfo         string          `json:"detailedInfo"`
	OptionYES            string          `json:"optionYES"`
	OptionNO             string          `json:"optionNO"`
	Keywords             []string        `json:"keywords,omitempty"`
	AuthoritativeSources []string        `json:"authoritativeSources,omitempty"`
	ResolutionRule       json.RawMessage `json:"resolutionRule,omitempty"`
	History              []HistoryPoint  `json:"-"`
}

type Client struct {
	gateway          string
	fallbackGateways []string
	httpClient       *http.Client
}

func NewClient(gateway string, fallbackGateways ...[]string) *Client {
	var fallbacks []string
	if len(fallbackGateways) > 0 {
		fallbacks = fallbackGateways[0]
	}
	return &Client{
		gateway:          gateway,
		fallbackGateways: dedupeGateways(fallbacks),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (c *Client) DownloadMetadata(cid string) (*Metadata, error) {
	cid = strings.TrimSpace(cid)
	if cid == "" {
		return &Metadata{}, nil
	}

	// 支持 inline-v1: 格式的 CID（内联编码）
	if strings.HasPrefix(cid, "inline-v1:") {
		return parseInlineCID(cid)
	}

	var attemptErrors []string
	for _, gateway := range c.gatewaysForCID(cid) {
		meta, err := c.downloadMetadataFromGateway(gateway, cid)
		if err == nil {
			return meta, nil
		}
		attemptErrors = append(attemptErrors, err.Error())
	}

	return nil, fmt.Errorf("download ipfs metadata for cid %s failed: %s", cid, strings.Join(attemptErrors, "; "))
}

func (c *Client) downloadMetadataFromGateway(gateway, cid string) (*Metadata, error) {
	url := joinGatewayCID(gateway, cid)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned HTTP %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var meta Metadata
	if err := json.Unmarshal(body, &meta); err != nil {
		return nil, fmt.Errorf("parse ipfs json: %w", err)
	}
	return &meta, nil
}

func (c *Client) gatewaysForCID(cid string) []string {
	gateways := []string{c.gateway}
	if looksLikeRemoteIPFSCID(cid) {
		gateways = append(gateways, c.fallbackGateways...)
	}
	return dedupeGateways(gateways)
}

func dedupeGateways(gateways []string) []string {
	seen := make(map[string]bool, len(gateways))
	result := make([]string, 0, len(gateways))
	for _, gateway := range gateways {
		key := strings.TrimRight(strings.TrimSpace(gateway), "/")
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, ensureTrailingSlash(gateway))
	}
	return result
}

func looksLikeRemoteIPFSCID(cid string) bool {
	cid = strings.TrimSpace(cid)
	return strings.HasPrefix(cid, "Qm") || strings.HasPrefix(cid, "baf") || strings.HasPrefix(cid, "zb2")
}

func joinGatewayCID(gateway, cid string) string {
	return strings.TrimRight(strings.TrimSpace(gateway), "/") + "/" + strings.TrimLeft(strings.TrimSpace(cid), "/")
}

func ensureTrailingSlash(gateway string) string {
	gateway = strings.TrimSpace(gateway)
	if strings.HasSuffix(gateway, "/") {
		return gateway
	}
	return gateway + "/"
}

func parseInlineCID(cid string) (*Metadata, error) {
	// 格式: inline-v1:002d323032362d30362d313520e887b320...
	// 去掉前缀
	hexData := strings.TrimPrefix(cid, "inline-v1:")

	// 解码 Hex
	data, err := hex.DecodeString(hexData)
	if err != nil {
		return nil, fmt.Errorf("decode inline hex: %w", err)
	}

	// 尝试解析为 JSON
	var meta Metadata
	if err := json.Unmarshal(data, &meta); err == nil {
		return &meta, nil
	}

	// 如果不是 JSON，可能是简单字符串格式，尝试提取 condition
	// 这种情况下我们可以构造一个基本的 Metadata
	meta = Metadata{
		Desc:      "Prediction Market",
		Condition: string(data),
		OptionYES: "YES",
		OptionNO:  "NO",
	}

	return &meta, nil
}
