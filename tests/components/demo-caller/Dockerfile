FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/caller .

FROM alpine:3.20
RUN adduser -D -u 10001 brickkit
COPY --from=build /out/caller /app/caller
USER 10001
EXPOSE 8080
ENTRYPOINT ["/app/caller"]
