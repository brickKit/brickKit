FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
RUN CGO_ENABLED=0 go build -trimpath -o /out/notify-webhook .

FROM alpine:3.20
RUN adduser -D -u 10001 brickkit
COPY --from=build /out/notify-webhook /app/notify-webhook
USER 10001
EXPOSE 8080
ENTRYPOINT ["/app/notify-webhook"]
