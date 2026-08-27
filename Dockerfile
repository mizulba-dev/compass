# --- build the SPA ---
FROM node:22-alpine AS web-build
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# --- build the Go binary ---
FROM golang:1.26-alpine AS go-build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
RUN CGO_ENABLED=0 go build -o /out/compass ./cmd/compass

# --- runtime ---
FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=go-build /out/compass ./compass
COPY --from=web-build /src/web/dist ./web/dist
ENV STATIC_DIR=/app/web/dist
EXPOSE 8080
ENTRYPOINT ["/app/compass"]
