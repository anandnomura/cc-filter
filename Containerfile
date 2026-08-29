FROM docker.io/library/golang:1.23-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY vendor ./vendor
COPY main.go ./
COPY cmd ./cmd
COPY bap-service ./bap-service
COPY configs ./configs
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go test -mod=vendor ./bap-service/... ./internal/authzen ./internal/auditwire ./internal/grants && \
    CGO_ENABLED=0 GOOS=linux go build -mod=vendor -trimpath -ldflags="-s -w" -o /out/bap-service ./bap-service/cmd

FROM docker.io/library/debian:bookworm-slim
RUN useradd --system --uid 10001 --home-dir /var/lib/bap bap
WORKDIR /app
COPY --from=build /out/bap-service /usr/local/bin/bap-service
COPY bap-service/policies /app/policies
RUN mkdir -p /var/lib/bap && chown -R bap:bap /var/lib/bap
USER 10001:10001
EXPOSE 8443
ENV BAP_LISTEN_ADDRESS=:8443 \
    BAP_POLICY_PATH=/app/policies/agent-tools.cedar \
    BAP_STATE_DIRECTORY=/var/lib/bap
ENTRYPOINT ["/usr/local/bin/bap-service"]
