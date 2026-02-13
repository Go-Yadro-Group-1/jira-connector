FROM golang:1.24-alpine
WORKDIR /app
COPY . .
RUN go mod download
RUN GOOS=linux GOARCH=amd64 go build -o jira-connector ./cmd/main.go

FROM alpine:latest
COPY --from=0 /app/jira-connector /usr/local/bin/jira-connector

EXPOSE 8080
EXPOSE 50051
EXPOSE 5432
EXPOSE 5672

ENTRYPOINT ["jira-connector"]
