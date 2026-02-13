FROM golang:1.25-alpine AS builder

RUN apk add --no-cache ca-certificates git upx

WORKDIR /app

RUN echo "appuser:x:65532:65532:App User:/:" > /etc_passwd && \
    echo "appuser:x:65532:" > /etc_group

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build \
    -ldflags="-w -s" \
    -trimpath \
    -o invoice-backend ./cmd/api

RUN upx --best --lzma invoice-backend

FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /etc_passwd /etc/passwd
COPY --from=builder /etc_group /etc/group
COPY --from=builder /app/invoice-backend /invoice-backend
COPY migrations/ /migrations/

USER appuser:appuser

EXPOSE 8080

ENTRYPOINT ["/invoice-backend"]
