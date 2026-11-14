FROM golang:alpine AS build
ENV GOTOOLCHAIN=auto
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /gateway ./cmd/gateway

FROM alpine:3.22
RUN apk add --no-cache ca-certificates
WORKDIR /app
RUN mkdir -p /app/data && chmod 777 /app/data && chown -R 65532:65532 /app
COPY --from=build --chown=65532:65532 /gateway /app/gateway
USER 65532:65532
ENTRYPOINT ["/app/gateway"]
