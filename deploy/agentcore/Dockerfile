# syntax=docker/dockerfile:1.7

FROM --platform=$BUILDPLATFORM golang:1.25-bookworm AS build

ARG TARGETOS=linux
ARG TARGETARCH=arm64

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY cmd/agentcore/voice-runtime ./cmd/agentcore/voice-runtime
COPY internal/voice/runtime ./internal/voice/runtime

RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w -buildid=" \
    -o /out/voice-runtime ./cmd/agentcore/voice-runtime

# base-debian12 retains the glibc runtime Task 9 will need for libopus while
# keeping the production stage free of a shell and package manager.
FROM --platform=$TARGETPLATFORM gcr.io/distroless/base-debian12:nonroot

COPY --from=build --chown=10001:10001 /out/voice-runtime /voice-runtime

USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["/voice-runtime"]
