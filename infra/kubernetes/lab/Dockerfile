FROM golang:1.25-alpine AS build
WORKDIR /src
COPY main.go .
RUN CGO_ENABLED=0 go build -trimpath -o /app main.go
FROM scratch
ARG BUILD_LABEL=local
ARG FAIL_READINESS=false
ENV LAB_VERSION=$BUILD_LABEL
ENV FAIL_READINESS=$FAIL_READINESS
COPY --from=build /app /app
USER 10001
EXPOSE 8080
ENTRYPOINT ["/app"]
