# syntax=docker/dockerfile:1.7@sha256:a57df69d0ea827fb7266491f2813635de6f17269be881f696fbfdf2d83dda33e

FROM --platform=$TARGETPLATFORM golang:1.25-bookworm@sha256:3b4a11519ad929d1e1d261a12cff056f0c85b735253d7d861346b9c6f8b36437 AS build

ARG TARGETOS=linux
ARG TARGETARCH=arm64

RUN test "$TARGETOS/$TARGETARCH" = "linux/arm64"
RUN apt-get update \
    && apt-get install -y --no-install-recommends libopus-dev pkg-config \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY cmd/agentcore/voice-runtime ./cmd/agentcore/voice-runtime
COPY internal ./internal
COPY pkg ./pkg

RUN go test -count=1 -tags="voice_libopus voice_libopus_test" ./internal/voice/audio
RUN CGO_ENABLED=1 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -tags=voice_libopus -trimpath -ldflags="-s -w -buildid=" \
    -o /out/voice-runtime ./cmd/agentcore/voice-runtime

FROM scratch AS voice-runtime-artifact

COPY --from=build /out/voice-runtime /voice-runtime

FROM --platform=$TARGETPLATFORM gcr.io/distroless/base-debian12:nonroot@sha256:7f0c72cd138b442ae0deeb69c08b1acf5525439ba251a49ad93c320a061567e5

COPY --from=build /usr/lib/aarch64-linux-gnu/libopus.so.0* /usr/lib/aarch64-linux-gnu/
COPY --from=build --chown=10001:10001 /out/voice-runtime /voice-runtime

USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["/voice-runtime"]
