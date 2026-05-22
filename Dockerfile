FROM golang:1.25-alpine AS build

WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY pkg/ pkg/
COPY internal/ internal/

ENV CGO_ENABLED=0 GOOS=linux
RUN go build -o /knowledge ./cmd/knowledge

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /knowledge /knowledge
USER 65532:65532
ENTRYPOINT ["/knowledge"]
