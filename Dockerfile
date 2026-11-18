FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY web ./web
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /memberos ./cmd/memberos

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 memberos
USER memberos
COPY --from=build /memberos /usr/local/bin/memberos
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/memberos"]
