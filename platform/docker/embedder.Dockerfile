FROM golang:1.24 AS build
ENV CGO_ENABLED=0

WORKDIR /app
COPY . .
RUN go build -o embedder-binary ./embedder

FROM alpine:3.21
COPY --from=build /app/embedder-binary /embedder-binary
COPY --from=build /app/config/config.yaml /config/config.yaml

WORKDIR /
CMD ["./embedder-binary"]

LABEL org.opencontainers.image.title="embedder" \
      org.opencontainers.image.authors="Ibrahim Konsowa <ibrahim@konsowa.com>"
