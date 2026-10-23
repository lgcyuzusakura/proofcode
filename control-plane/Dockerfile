FROM golang:1.24-alpine AS mcp-inspect
WORKDIR /src
ENV GOPROXY=https://goproxy.cn,direct
COPY agent-engine/go.mod agent-engine/go.sum ./
RUN go mod download
COPY agent-engine/cmd/proofcode-mcp-inspect ./cmd/proofcode-mcp-inspect
COPY agent-engine/internal ./internal
COPY agent-engine/pkg ./pkg
RUN CGO_ENABLED=0 go build -trimpath -o /proofcode-mcp-inspect ./cmd/proofcode-mcp-inspect

FROM maven:3.9-eclipse-temurin-17 AS build
WORKDIR /src
COPY control-plane/settings.xml /root/.m2/settings.xml
COPY control-plane/pom.xml .
COPY control-plane/src src
RUN mvn -q -DskipTests package

FROM eclipse-temurin:17-jre-alpine
RUN apk add --no-cache ca-certificates && addgroup -S proofcode && adduser -S proofcode -G proofcode
COPY --from=mcp-inspect /proofcode-mcp-inspect /usr/local/bin/proofcode-mcp-inspect
USER proofcode
COPY --from=build /src/target/control-plane-0.4.0.jar /app.jar
ENTRYPOINT ["java", "-XX:MaxRAMPercentage=75", "-jar", "/app.jar"]
