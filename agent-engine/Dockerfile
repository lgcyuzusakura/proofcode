FROM golang:1.24-alpine AS build
WORKDIR /src
ENV GOPROXY=https://goproxy.cn,direct
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/proofcode-runner ./cmd/runner

FROM alpine:3.21
RUN apk add --no-cache ca-certificates git ripgrep && addgroup -S proofcode && adduser -S proofcode -G proofcode
COPY --from=build /out/proofcode-runner /usr/local/bin/proofcode-runner
USER proofcode
ENTRYPOINT ["proofcode-runner"]
