FROM golang:1.24-alpine
WORKDIR /app
COPY . .
RUN go mod download
RUN GOOS=linux GOARCH=amd64 go build -o jira-connector-server ./cmd/main.go

FROM alpine:latest
COPY --from=0 /app/jira-connector-server /usr/local/bin/jira-connector-server

EXPOSE 8080
EXPOSE 50051
EXPOSE 5432

ENTRYPOINT ["jira-connector-server"]
