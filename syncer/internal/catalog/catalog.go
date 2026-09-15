// Package catalog describes read-only data coverage, not freshness or sync jobs.
package catalog

import "context"

type Item struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Status      string `json:"status"`
	SyncEnabled *bool  `json:"syncEnabled,omitempty"`
	Rows        *int64 `json:"rows"`
	StartDate   string `json:"startDate"`
	EndDate     string `json:"endDate"`
	Note        string `json:"note,omitempty"`
}
type Group struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Items   []Item `json:"items"`
	Message string `json:"message,omitempty"`
}
type Result struct {
	Groups []Group `json:"groups"`
}
type Reader interface{ Catalog(context.Context) Result }
