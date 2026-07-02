package ipfs

import (
	"encoding/json"
	"math"
	"sort"
)

const percentageSumTolerance = 0.5

type HistoryPoint struct {
	Time       int64   `json:"time"`
	YesPercent float64 `json:"yes_percent"`
	NoPercent  float64 `json:"no_percent"`
}

func (m *Metadata) UnmarshalJSON(data []byte) error {
	var raw struct {
		Desc                 string            `json:"desc"`
		Condition            string            `json:"condition"`
		AvatarURL            string            `json:"avatarUrl"`
		DetailedInfo         string            `json:"detailedInfo"`
		OptionYES            string            `json:"optionYES"`
		OptionNO             string            `json:"optionNO"`
		Keywords             []string          `json:"keywords"`
		AuthoritativeSources []string          `json:"authoritativeSources"`
		History              []json.RawMessage `json:"history"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	*m = Metadata{
		Desc:                 raw.Desc,
		Condition:            raw.Condition,
		AvatarURL:            raw.AvatarURL,
		DetailedInfo:         raw.DetailedInfo,
		OptionYES:            raw.OptionYES,
		OptionNO:             raw.OptionNO,
		Keywords:             raw.Keywords,
		AuthoritativeSources: raw.AuthoritativeSources,
		History:              normalizeHistory(raw.History),
	}
	return nil
}

func normalizeHistory(rawPoints []json.RawMessage) []HistoryPoint {
	byTime := make(map[int64]HistoryPoint, len(rawPoints))
	for _, data := range rawPoints {
		point, ok := parseHistoryPoint(data)
		if ok {
			byTime[point.Time] = point
		}
	}

	points := make([]HistoryPoint, 0, len(byTime))
	for _, point := range byTime {
		points = append(points, point)
	}
	sort.Slice(points, func(i, j int) bool {
		return points[i].Time < points[j].Time
	})
	return points
}

func parseHistoryPoint(data []byte) (HistoryPoint, bool) {
	var raw struct {
		T    *int64   `json:"t"`
		Time *int64   `json:"time"`
		Y    *float64 `json:"y"`
		Yes  *float64 `json:"yes"`
		N    *float64 `json:"n"`
		No   *float64 `json:"no"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return HistoryPoint{}, false
	}

	timestamp := raw.Time
	if raw.T != nil {
		timestamp = raw.T
	}
	if timestamp == nil || *timestamp <= 0 {
		return HistoryPoint{}, false
	}

	yes := raw.Yes
	if raw.Y != nil {
		yes = raw.Y
	}
	no := raw.No
	if raw.N != nil {
		no = raw.N
	}
	if yes == nil && no == nil {
		return HistoryPoint{}, false
	}
	if (yes != nil && !validPercentage(*yes)) || (no != nil && !validPercentage(*no)) {
		return HistoryPoint{}, false
	}

	var yesPercent, noPercent float64
	switch {
	case yes == nil:
		noPercent = *no
		yesPercent = 100 - noPercent
	case no == nil:
		yesPercent = *yes
		noPercent = 100 - yesPercent
	default:
		yesPercent = *yes
		noPercent = *no
		sum := yesPercent + noPercent
		if math.Abs(sum-100) > percentageSumTolerance || sum == 0 {
			return HistoryPoint{}, false
		}
		if sum != 100 {
			yesPercent = yesPercent / sum * 100
			noPercent = 100 - yesPercent
		}
	}

	return HistoryPoint{
		Time:       *timestamp,
		YesPercent: yesPercent,
		NoPercent:  noPercent,
	}, true
}

func validPercentage(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 100
}
