package rotation

import (
	"math"
	"time"
)

type DailyCard struct {
	SourceTime string            `json:"sourceTime,omitempty"`
	CapturedAt string            `json:"capturedAt,omitempty"`
	Code       string            `json:"code"`
	Name       string            `json:"name"`
	Price      *float64          `json:"price"`
	Score      *float64          `json:"score"`
	Rank       *int              `json:"rank"`
	Volatility *float64          `json:"volatility"`
	Quantile   *float64          `json:"quantile"`
	Weight     *float64          `json:"weight"`
	Reasons    map[string]string `json:"reasons"`
}

type DailyResult struct {
	TradeDate      string      `json:"tradeDate"`
	Basis          string      `json:"basis"`
	Available      int         `json:"available"`
	PublishedAt    string      `json:"publishedAt"`
	Source         string      `json:"source"`
	Version        string      `json:"version"`
	QuantileWindow int         `json:"quantileWindow"`
	Cards          []DailyCard `json:"cards"`
}

type DailyStage struct {
	Status    string         `json:"status"`
	Available int            `json:"available"`
	Message   string         `json:"message"`
	UpdatedAt string         `json:"updatedAt"`
	Missing   []DailyMissing `json:"missing"`
}
type DailyMissing struct {
	Code   string `json:"code"`
	Reason string `json:"reason"`
}
type DailyProgress struct {
	TradeDate string     `json:"tradeDate"`
	Reference DailyStage `json:"reference"`
	Close     DailyStage `json:"close"`
}
type PriceSlippage struct {
	Code   string   `json:"code"`
	Bps    *float64 `json:"bps"`
	Reason string   `json:"reason"`
}
type DailyView struct {
	RequestedDate      string          `json:"requestedDate"`
	CurrentDate        string          `json:"currentDate"`
	CurrentTradingDate string          `json:"currentTradingDate"`
	SelectionMode      string          `json:"selectionMode"`
	Fallback           bool            `json:"fallback"`
	FallbackReason     string          `json:"fallbackReason"`
	CalendarStatus     string          `json:"calendarStatus"`
	ReferenceState     DailyStage      `json:"referenceState"`
	CloseState         DailyStage      `json:"closeState"`
	Pending            *DailyProgress  `json:"pending,omitempty"`
	PriceSlippage      []PriceSlippage `json:"priceSlippage"`
	TradeDate          string          `json:"tradeDate"`
	Status             string          `json:"status"`
	Message            string          `json:"message"`
	Available          int             `json:"available"`
	Close              *DailyResult    `json:"close"`
	Reference          *DailyResult    `json:"reference"`
	ReferenceStatus    string          `json:"referenceStatus"`
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
func (s *Service) today() string {
	return s.now().In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("20060102")
}
func (s *Service) quantileWindow() int {
	if s.QuantileWindow > 0 {
		return s.QuantileWindow
	}
	return 1200
}
func finiteNumber(v float64) *float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	return &v
}
