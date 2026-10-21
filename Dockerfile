FROM golang:1.26.2 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -o /inbox ./cmd/inbox
RUN mkdir /state

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /inbox /inbox
COPY --from=build --chown=65532:65532 /state /state
USER 65532:65532
ENV APP_ADDR=0.0.0.0:8087
ENV AI_REQUEST_LEDGER=/state/ai-requests.json
EXPOSE 8087
STOPSIGNAL SIGINT
ENTRYPOINT ["/inbox"]
