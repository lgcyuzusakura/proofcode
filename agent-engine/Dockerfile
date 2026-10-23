FROM golang:1.24-alpine AS build
WORKDIR /src
ENV GOPROXY=https://goproxy.cn,direct
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/proofcode-runner ./cmd/runner
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/proofcode-apply ./cmd/proofcode-apply
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/proofcode-mcp ./cmd/proofcode-mcp

FROM golang:1.24-alpine
RUN apk add --no-cache ca-certificates git ripgrep nodejs npm python3 py3-pytest su-exec chromium \
    && addgroup -S proofcode && adduser -S proofcode -G proofcode \
    && mkdir -p /workspaces
COPY --from=build /out/proofcode-runner /usr/local/bin/proofcode-runner
COPY --from=build /out/proofcode-apply /usr/local/bin/proofcode-apply
COPY --from=build /out/proofcode-mcp /usr/local/bin/proofcode-mcp
COPY runner-entrypoint.sh /usr/local/bin/runner-entrypoint
RUN sed -i 's/\r$//' /usr/local/bin/runner-entrypoint && chmod +x /usr/local/bin/runner-entrypoint
ENTRYPOINT ["runner-entrypoint"]
