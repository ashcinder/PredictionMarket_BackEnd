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
	Desc         string         `json:"desc"`
	Condition    string         `json:"condition"`
	AvatarURL    string         `json:"avatarUrl"`
	DetailedInfo string         `json:"detailedInfo"`
	OptionYES    string         `json:"optionYES"`
	OptionNO     string         `json:"optionNO"`
	History      []HistoryPoint `json:"-"`
}

type Client struct {
	gateway    string
	httpClient *http.Client
}

func NewClient(gateway string) *Client {
	return &Client{
		gateway: gateway,
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

	// 普通 IPFS CID
	url := c.gateway + cid
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ipfs gateway HTTP %d for cid %s", resp.StatusCode, cid)
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
		Desc:      "博弈池",
		Condition: string(data),
		OptionYES: "YES",
		OptionNO:  "NO",
	}

	return &meta, nil
}
