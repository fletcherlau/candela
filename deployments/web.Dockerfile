# Build from the repository root; no secrets enter the image.
FROM node:24-alpine AS frontend
WORKDIR /src
COPY web/frontend/package.json web/frontend/package-lock.json ./
RUN npm ci
COPY web/frontend/ ./
RUN npm run build

FROM golang:1.26 AS build
WORKDIR /src
COPY web/go.mod web/go.sum ./
RUN go mod download
COPY web/ ./
RUN CGO_ENABLED=0 go build -trimpath -o /out/candela-web .

FROM alpine:3.20
WORKDIR /app
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/candela-web /app/candela-web
COPY --from=frontend /src/dist /app/frontend/dist
COPY etf-rotation/dca-dashboard/index.html etf-rotation/dca-dashboard/no-valve.html etf-rotation/dca-dashboard/data.js etf-rotation/dca-dashboard/data_valveon.js /app/research/
ENV WEB_ADDR=0.0.0.0:8080 WEB_RESEARCH_DIR=/app/research
USER 65532:65532
CMD ["/app/candela-web"]
