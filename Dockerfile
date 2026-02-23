FROM golang:1.26.0-alpine as builder
WORKDIR /app/
COPY . .
RUN CGO_ENABLED=0 go build -o bin/links main.go
RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o bin/links-arm64 .

FROM alpine:3.23
WORKDIR /app/
COPY --from=builder /app/bin/links .
COPY --from=builder /app/bin/links-arm64 .
COPY entrypoint.sh .
RUN chmod +x /app/entrypoint.sh

EXPOSE 8080
ENTRYPOINT ["/app/entrypoint.sh"]
CMD ["serve", "--http=0.0.0.0:8080"]
