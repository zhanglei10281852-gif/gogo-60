# syntax=docker/dockerfile:1

# ---- builder -----------------------------------------------------------------
# The build stage is fully offline: no module downloads are needed because
# CaveLoop depends on the Go standard library only. GOFLAGS pins the module mode
# and GOPROXY is disabled so a network fetch can never be attempted.
FROM golang:1.22 AS builder

ENV GOTOOLCHAIN=local \
    CGO_ENABLED=0 \
    GOOS=linux \
    GOPROXY=off \
    GOFLAGS=-mod=mod

WORKDIR /src

COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal

RUN go build -trimpath -ldflags="-s -w" -o /out/caveloop ./cmd/caveloop

# ---- runtime -----------------------------------------------------------------
# The final image carries nothing but the statically linked binary.
FROM scratch

COPY --from=builder /out/caveloop /caveloop

ENTRYPOINT ["/caveloop"]
