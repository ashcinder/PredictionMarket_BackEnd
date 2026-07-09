package scenario

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	TypePrice          = "TYPE_PRICE"
	TypeVolatility     = "TYPE_VOLATILITY"
	TypeVolume         = "TYPE_VOLUME"
	TypeTechnical      = "TYPE_TECHNICAL"
	TypeTouch          = "TYPE_TOUCH"
	TypeRelative       = "TYPE_RELATIVE"
	TypePriceThreshold = "TYPE_PRICE_THRESHOLD"
	TypeEvent          = "TYPE_EVENT"
)

type Template struct {
	Type         string
	Title        string
	OptionYes    string
	OptionNo     string
	Params       []string
	BuildDesc    func(param string, end time.Time, seed int) string
	BuildCond    func(param string, end time.Time, seed int) string
	DetailedInfo string
}

type Market struct {
	Type             string
	IPFSCID          string
	Desc             string
	Condition        string
	AvatarURL        string
	DetailedInfo     string
	OptionYes        string
	OptionNo         string
	CreatorAddress   string
	DurationSeconds  int64
	InitialLiquidity string
	MetadataJSON     string
}

type BuildMarketInput struct {
	Type         string
	Index        int
	Creator      string
	Duration     time.Duration
	InitialBKC   string
	Now          time.Time
	TemplateSeed int
}

var templates = map[string]Template{
	TypePrice: {
		Type:      TypePrice,
		Title:     "黄金价格方向预测",
		OptionYes: "预测成立 (YES)",
		OptionNo:  "预测不成立 (NO)",
		Params:    []string{"上涨", "下跌", "持平"},
		BuildDesc: func(param string, end time.Time, _ int) string {
			return fmt.Sprintf("到 %s 黄金价格是否%s", end.Format("2006-01-02 15:04"), param)
		},
		BuildCond: func(param string, end time.Time, _ int) string {
			return fmt.Sprintf("黄金价格在截止时间 %s 的方向为%s", end.Format("2006-01-02 15:04"), param)
		},
		DetailedInfo: "模拟用户创建的方向类黄金预测池。",
	},
	TypeVolatility: {
		Type:      TypeVolatility,
		Title:     "黄金波动率预测",
		OptionYes: "波动达标 (YES)",
		OptionNo:  "波动未达标 (NO)",
		Params:    []string{"1.5", "3", "5"},
		BuildDesc: func(param string, end time.Time, _ int) string {
			return fmt.Sprintf("到 %s 黄金价格波动是否达到 %s%%", end.Format("2006-01-02 15:04"), param)
		},
		BuildCond: func(param string, end time.Time, _ int) string {
			return fmt.Sprintf("黄金价格波动幅度 >= %s%%，观察期截至 %s", param, end.Format("2006-01-02 15:04"))
		},
		DetailedInfo: "模拟用户创建的波动率类黄金预测池。",
	},
	TypeVolume: {
		Type:      TypeVolume,
		Title:     "黄金交易量预测",
		OptionYes: "交易量达标 (YES)",
		OptionNo:  "交易量未达标 (NO)",
		Params:    []string{"300", "500", "800"},
		BuildDesc: func(param string, end time.Time, seed int) string {
			return fmt.Sprintf("%s 黄金交易量是否%s %s 吨", end.Format("2006-01-02"), operatorForSeed(seed), param)
		},
		BuildCond: func(param string, end time.Time, seed int) string {
			return fmt.Sprintf("黄金交易量 %s %s 吨，观察日 %s", operatorForSeed(seed), param, end.Format("2006-01-02"))
		},
		DetailedInfo: "模拟用户创建的交易量类黄金预测池。",
	},
	TypeTechnical: {
		Type:      TypeTechnical,
		Title:     "黄金技术指标预测",
		OptionYes: "指标条件成立 (YES)",
		OptionNo:  "指标条件不成立 (NO)",
		Params:    []string{"RSI 高于 70", "MACD 上穿信号线", "KDJ 死叉", "BOLL 跌破下轨"},
		BuildDesc: func(param string, end time.Time, _ int) string {
			return fmt.Sprintf("到 %s 黄金技术指标是否出现：%s", end.Format("2006-01-02 15:04"), param)
		},
		BuildCond: func(param string, end time.Time, _ int) string {
			return fmt.Sprintf("黄金技术指标条件 %s，观察期截至 %s", param, end.Format("2006-01-02 15:04"))
		},
		DetailedInfo: "模拟用户创建的技术指标类黄金预测池。",
	},
	TypeTouch: {
		Type:      TypeTouch,
		Title:     "黄金触价预测",
		OptionYes: "触及价格 (YES)",
		OptionNo:  "未触及价格 (NO)",
		Params:    []string{"2500", "2800", "3000"},
		BuildDesc: func(param string, end time.Time, _ int) string {
			return fmt.Sprintf("黄金是否会在 %s 前触及 %s USD", end.Format("2006-01-02 15:04"), param)
		},
		BuildCond: func(param string, end time.Time, _ int) string {
			return fmt.Sprintf("黄金价格在观察期内触及 %s USD，观察期截至 %s", param, end.Format("2006-01-02 15:04"))
		},
		DetailedInfo: "模拟用户创建的触价类黄金预测池。",
	},
	TypeRelative: {
		Type:      TypeRelative,
		Title:     "黄金相对表现预测",
		OptionYes: "黄金跑赢 (YES)",
		OptionNo:  "黄金未跑赢 (NO)",
		Params:    []string{"BTC", "S&P 500", "白银"},
		BuildDesc: func(param string, end time.Time, _ int) string {
			return fmt.Sprintf("到 %s 黄金表现是否跑赢 %s", end.Format("2006-01-02 15:04"), param)
		},
		BuildCond: func(param string, end time.Time, _ int) string {
			return fmt.Sprintf("黄金收益率高于 %s，观察期截至 %s", param, end.Format("2006-01-02 15:04"))
		},
		DetailedInfo: "模拟用户创建的相对表现类黄金预测池。",
	},
	TypePriceThreshold: {
		Type:      TypePriceThreshold,
		Title:     "黄金价格阈值预测",
		OptionYes: "价格满足条件 (YES)",
		OptionNo:  "价格不满足条件 (NO)",
		Params:    []string{"2800", "3000", "3200"},
		BuildDesc: func(param string, end time.Time, seed int) string {
			return fmt.Sprintf("到 %s 黄金价格是否%s %s USD", end.Format("2006-01-02 15:04"), operatorForSeed(seed), param)
		},
		BuildCond: func(param string, end time.Time, seed int) string {
			return fmt.Sprintf("到期时黄金价格 %s %s USD，截止时间 %s", operatorForSeed(seed), param, end.Format("2006-01-02 15:04"))
		},
		DetailedInfo: "模拟用户创建的到期价格阈值类黄金预测池。",
	},
	TypeEvent: {
		Type:      TypeEvent,
		Title:     "宏观事件预测",
		OptionYes: "事件发生 (YES)",
		OptionNo:  "事件未发生 (NO)",
		Params:    []string{"美联储降息", "CPI 低于预期", "美元指数大幅回落"},
		BuildDesc: func(param string, end time.Time, _ int) string {
			return fmt.Sprintf("%s 前是否发生：%s", end.Format("2006-01-02 15:04"), param)
		},
		BuildCond: func(param string, end time.Time, _ int) string {
			return fmt.Sprintf("事件“%s”在 %s 前发生", param, end.Format("2006-01-02 15:04"))
		},
		DetailedInfo: "模拟用户创建的宏观事件类黄金预测池。",
	},
}

func SupportedTypes() []string {
	return []string{
		TypePrice,
		TypeVolatility,
		TypeVolume,
		TypeTechnical,
		TypeTouch,
		TypeRelative,
		TypePriceThreshold,
		TypeEvent,
	}
}

func TemplateForType(typ string) (Template, bool) {
	tpl, ok := templates[strings.TrimSpace(typ)]
	return tpl, ok
}

func BuildMarket(input BuildMarketInput) (*Market, error) {
	tpl, ok := TemplateForType(input.Type)
	if !ok {
		return nil, fmt.Errorf("unsupported market type %q", input.Type)
	}
	if input.Now.IsZero() {
		input.Now = time.Now().UTC()
	}
	if input.Duration <= 0 {
		return nil, fmt.Errorf("duration must be positive")
	}
	param := tpl.Params[input.TemplateSeed%len(tpl.Params)]
	end := input.Now.Add(input.Duration)
	desc := tpl.BuildDesc(param, end, input.TemplateSeed)
	condition := tpl.BuildCond(param, end, input.TemplateSeed)
	metadata := map[string]any{
		"type":          tpl.Type,
		"desc":          desc,
		"condition":     condition,
		"avatarUrl":     "",
		"detailedInfo":  tpl.DetailedInfo,
		"optionYES":     tpl.OptionYes,
		"optionNO":      tpl.OptionNo,
		"creator":       input.Creator,
		"initialBKC":    input.InitialBKC,
		"durationSec":   int64(input.Duration / time.Second),
		"templateParam": param,
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	return &Market{
		Type:             tpl.Type,
		IPFSCID:          "sim-" + hex.EncodeToString(sum[:])[:32],
		Desc:             desc,
		Condition:        condition,
		DetailedInfo:     tpl.DetailedInfo,
		OptionYes:        tpl.OptionYes,
		OptionNo:         tpl.OptionNo,
		CreatorAddress:   input.Creator,
		DurationSeconds:  int64(input.Duration / time.Second),
		InitialLiquidity: input.InitialBKC,
		MetadataJSON:     string(raw),
	}, nil
}

func operatorForSeed(seed int) string {
	switch seed % 3 {
	case 1:
		return "低于"
	case 2:
		return "等于"
	default:
		return "高于"
	}
}
