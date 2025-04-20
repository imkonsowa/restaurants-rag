FROM golang:1.24 AS build
ENV CGO_ENABLED=1

WORKDIR /app
COPY . .
RUN go build -o agent-binary ./agent

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=build /app/agent-binary /agent-binary
COPY --from=build /app/config/config.yaml /config/config.yaml
COPY --from=build /app/web/ /web

WORKDIR /
CMD ["./agent-binary"]

LABEL org.opencontainers.image.title="agent" \
      org.opencontainers.image.authors="Ibrahim Konsowa <ibrahim@konsowa.com>"
