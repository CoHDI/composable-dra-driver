FROM docker.io/golang:1.24.1 AS builder
WORKDIR /workspace

COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download

COPY main.go main.go
COPY pkg pkg
RUN go build

FROM docker.io/golang:1.24.1
WORKDIR /
COPY --from=builder /workspace/cdi_dra .

RUN useradd nonroot
USER nonroot

ENTRYPOINT ["/cdi_dra"]
