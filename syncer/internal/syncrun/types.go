// Package syncrun owns durable acceptance, object serialization and transactional
// progress for index synchronization. It has no ETF or strategy dependencies.
package syncrun

import (
	"context"
	"errors"
	"time"
)

const Code = "000985.CSI"
const HistoryFloor = "00010101" // unbounded calendar range; not an assumed index inception date
const WindowDays = 366

var ErrCancelled = errors.New("cancellation requested")

var ErrOwnership = errors.New("execution ownership expired")
var ErrMode = errors.New("mode must be backfill or incremental")

type Event struct {
	Kind       string `json:"kind"`
	At         string `json:"at"`
	Checkpoint string `json:"checkpoint"`
	Message    string `json:"message"`
}

type Run struct {
	Events            []Event `json:"events,omitempty"`
	ID                string  `json:"id"`
	Code              string  `json:"code"`
	Mode              string  `json:"mode"`
	StartDate         string  `json:"startDate"`
	EndDate           string  `json:"endDate"`
	EffectiveStart    string  `json:"effectiveStart"`
	State             string  `json:"state"`
	Stage             string  `json:"stage"`
	ProcessedRows     int64   `json:"processedRows"`
	CompletedSegments int     `json:"completedSegments"`
	TotalSegments     int     `json:"totalSegments"`
	Checkpoint        string  `json:"checkpoint"`
	HistoryEvidence   string  `json:"historyEvidence"`
	ErrorCode         string  `json:"errorCode"`
	Message           string  `json:"message"`
	CreatedAt         string  `json:"createdAt"`
	UpdatedAt         string  `json:"updatedAt"`
	Owner             int64   `json:"-"`
}

type Bar struct {
	Code  string  `json:"ts_code"`
	Date  string  `json:"trade_date"`
	Open  float64 `json:"open"`
	High  float64 `json:"high"`
	Low   float64 `json:"low"`
	Close float64 `json:"close"`
	// Source inception records can omit previous-session metrics and turnover.
	// Preserve their absence through decoding and SQL; never manufacture zeros.
	PreClose  *float64 `json:"pre_close"`
	Change    *float64 `json:"change"`
	PctChange *float64 `json:"pct_chg"`
	Volume    *float64 `json:"vol"`
	Amount    *float64 `json:"amount"`
}

type Source interface {
	// Earliest proves there are no earlier source rows by an empty historical probe.
	Earliest(context.Context, string) (date, evidence string, err error)
	// Window must return complete, validated data, detecting truncation and gaps.
	Window(context.Context, string, string) ([]Bar, error)
}

type SourceError struct{ Code, Message string }

func (e *SourceError) Error() string { return e.Message }

// Cutoff freezes the daily boundary at submission. Before the 18:00 Shanghai
// publication boundary only yesterday is eligible; weekends are harmless.
func Cutoff(now time.Time) string {
	local := now.In(time.FixedZone("Asia/Shanghai", 8*60*60))
	if local.Hour() < 18 {
		local = local.AddDate(0, 0, -1)
	}
	return local.Format("20060102")
}
func date(s string) time.Time { t, _ := time.Parse("20060102", s); return t }
