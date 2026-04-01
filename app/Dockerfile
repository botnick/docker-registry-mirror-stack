FROM golang:1.25-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/registry-control .

FROM registry:2.8.3 AS registry-bin

FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates wget \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=build /out/registry-control /usr/local/bin/registry-control
COPY --from=registry-bin /bin/registry /usr/local/bin/registry
CMD ["registry-control"]
