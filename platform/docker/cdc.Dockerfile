FROM golang:1.24 AS build
ENV CGO_ENABLED=0

WORKDIR /app
COPY . .
RUN go build -o cdc-binary ./cdc

FROM alpine:3.21
COPY --from=build /app/cdc-binary /cdc-binary
COPY --from=build /app/config/config.yaml /config/config.yaml

WORKDIR /
CMD ["./cdc-binary"]

LABEL org.opencontainers.image.title="cdc" \
      org.opencontainers.image.authors="Ibrahim Konsowa <ibrahim@konsowa.com>"
