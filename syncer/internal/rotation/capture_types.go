package rotation

import (
	"syncer/internal/core"
	"time"
)

var shanghai = time.FixedZone("Asia/Shanghai", 8*3600)

const indicatorVersion = "er20-yz20-240-q70-40-v1"

type CaptureParams struct {
	Version        string `json:"version"`
	QuantileWindow int    `json:"quantileWindow"`
}
type CaptureRun struct {
	TradeDate  string        `json:"tradeDate"`
	TargetAt   string        `json:"targetAt"`
	DeadlineAt string        `json:"deadlineAt"`
	Basis      string        `json:"basis"`
	State      string        `json:"state"`
	Stage      string        `json:"stage"`
	Available  int           `json:"available"`
	Message    string        `json:"message"`
	Params     CaptureParams `json:"params"`
	Recoveries int           `json:"recoveries"`
	CreatedAt  string        `json:"createdAt"`
	UpdatedAt  string        `json:"updatedAt"`
	Items      []CaptureItem `json:"items,omitempty"`
	Owner      int64         `json:"-"`
}
type CaptureItem struct {
	Code   string        `json:"code"`
	Name   string        `json:"name"`
	State  string        `json:"state"`
	Reason string        `json:"reason"`
	Input  *CaptureInput `json:"input,omitempty"`
}
type FrozenBar struct {
	Date       string   `json:"date"`
	Open       *float64 `json:"open"`
	High       *float64 `json:"high"`
	Low        *float64 `json:"low"`
	Close      *float64 `json:"close"`
	Factor     *float64 `json:"factor"`
	FactorDate string   `json:"factorDate"`
}
type FrozenCalendarDay struct {
	Suspended bool   `json:"suspended,omitempty"`
	Date      string `json:"date"`
	Open      bool   `json:"open"`
}
type CaptureInput struct {
	Quote           core.RealtimeQuote  `json:"quote"`
	RequestedAt     time.Time           `json:"requestedAt"`
	CapturedAt      time.Time           `json:"capturedAt"`
	Params          CaptureParams       `json:"params"`
	History         []FrozenBar         `json:"history"`
	Factor          *float64            `json:"factor"`
	FactorDate      string              `json:"factorDate"`
	Calendar        []FrozenCalendarDay `json:"calendar"`
	CoverageThrough string              `json:"coverageThrough"`
}

func captureTarget(date string) (time.Time, error) {
	return time.ParseInLocation("2006010215:04", date+"14:45", shanghai)
}
