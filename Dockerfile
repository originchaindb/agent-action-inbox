FROM golang:1.26.2 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -o /inbox ./cmd/inbox

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /inbox /inbox
USER 65532:65532
ENV APP_ADDR=0.0.0.0:8087
EXPOSE 8087
STOPSIGNAL SIGINT
ENTRYPOINT ["/inbox"]
