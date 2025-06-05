FROM golang:1.20-alpine

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY web/serve.go ./
RUN go build -o report-server ./serve.go

EXPOSE 8080

CMD ["./report-server", "-port", "8080", "-results", "/app/results"]
