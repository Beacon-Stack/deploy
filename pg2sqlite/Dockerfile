# pg2sqlite — one-shot Postgres → SQLite migrator for the Beacon Stack upgrade.
# See deploy/README.md "Upgrading from Postgres" for the full workflow.

FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY main.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags "-s -w" -o /pg2sqlite .

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=build /pg2sqlite /usr/local/bin/pg2sqlite
ENTRYPOINT ["/usr/local/bin/pg2sqlite"]
