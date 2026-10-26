FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod main.go main_test.go ./
RUN go test ./... && CGO_ENABLED=0 go build -trimpath -o /application .
FROM alpine:3.22
RUN adduser -D -u 10001 app
COPY --from=build /application /application
USER 10001
EXPOSE 8080
ENTRYPOINT ["/application"]
