FROM golang:1.22-alpine as builder
WORKDIR /app/
COPY . .
RUN CGO_ENABLED=0 go build -o bin/links main.go

FROM alpine:3.20
WORKDIR /app/
COPY --from=builder /app/bin/links .
ENTRYPOINT ["./links"]
