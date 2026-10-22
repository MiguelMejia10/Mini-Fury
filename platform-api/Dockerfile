FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go test ./... && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /platform-api ./cmd/server && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /platform-user ./cmd/user
FROM alpine:3.22
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 platform
COPY --from=build /platform-api /usr/local/bin/platform-api
COPY --from=build /platform-user /usr/local/bin/platform-user
USER platform
EXPOSE 8080
ENTRYPOINT ["platform-api"]
