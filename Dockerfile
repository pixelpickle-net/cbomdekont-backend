FROM golang:1.22-alpine as builder

WORKDIR /app

COPY go.mod ./
RUN go mod download

COPY . .

RUN go build -o server cmd/api/main.go

FROM alpine:latest  
RUN apk --no-cache add ca-certificates

WORKDIR /root/

COPY --from=builder /app/server .
COPY --from=builder /app/schema.json .

CMD ["./server"]
