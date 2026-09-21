package rotation

import (
	"math"
	"time"
)

type DailyCard struct {
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

type DailyView struct {
	TradeDate       string       `json:"tradeDate"`
	Status          string       `json:"status"`
	Message         string       `json:"message"`
	Available       int          `json:"available"`
	Close           *DailyResult `json:"close"`
	Reference       *DailyResult `json:"reference"`
	ReferenceStatus string       `json:"referenceStatus"`
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
