package handler

import (
	"encoding/json"
	"github.com/zeromicro/go-zero/rest"
	"net/http"
	"syncer/internal/catalog"
	"syncer/internal/svc"
)

// RegisterCatalog keeps the hand-written read-only extension separate from goctl routes.
func RegisterCatalog(server *rest.Server, ctx *svc.ServiceContext, reader catalog.Reader) {
	server.AddRoute(rest.Route{Method: http.MethodGet, Path: "/api/v1/data/catalog", Handler: ctx.ApiKeyAuth(CatalogHandler(reader))})
}
func CatalogHandler(reader catalog.Reader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(reader.Catalog(r.Context()))
	}
}
