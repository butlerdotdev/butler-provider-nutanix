# syntax=docker/dockerfile:1.6

# Butler Provider Nutanix Controller
FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS builder

WORKDIR /workspace
RUN apk add --no-cache git make

ARG TARGETOS
ARG TARGETARCH

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 \
    GOOS=${TARGETOS} \
    GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /workspace/manager cmd/main.go

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /workspace/manager /manager
USER 65532:65532
ENTRYPOINT ["/manager"]
