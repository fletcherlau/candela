package rotation

import (
	"github.com/zeromicro/go-zero/rest"
	"net/http"
)

func (s *Service) DailyRoutes(auth func(http.HandlerFunc) http.HandlerFunc) []rest.Route {
	return []rest.Route{{Method: http.MethodGet, Path: "/api/v1/rotation/daily", Handler: auth(s.DailyHandler().ServeHTTP)}}
}
