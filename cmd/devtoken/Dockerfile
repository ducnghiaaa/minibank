# Local only. Build from the repo root: docker build -f cmd/devtoken/Dockerfile .
FROM --platform=$BUILDPLATFORM golang:1.26 AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -o /out/devtoken ./cmd/devtoken

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/devtoken /devtoken
EXPOSE 9000
ENTRYPOINT ["/devtoken"]
