# kubeops image build (TARGET=console|oidc|nfs → kubeops-console|authentication|nfs).
# Example:
#   docker build --build-arg TARGET=console -t kubeops-console:1.3.0 .
#   docker build --build-arg TARGET=oidc -t kubeops-authentication:1.3.0 .
#   docker build --build-arg TARGET=nfs -t kubeops-nfs:1.3.0 .
ARG TARGET=console
FROM golang:1.22-alpine AS builder
ARG TARGET
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o /out/manager ./cmd/${TARGET}

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /out/manager /manager
USER 65532:65532
ENTRYPOINT ["/manager"]
