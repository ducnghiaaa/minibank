# Build from the repo root: docker build -f services/account/Dockerfile .
FROM --platform=$BUILDPLATFORM golang:1.26 AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/account ./services/account/cmd/account

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/account /account
EXPOSE 8080
ENTRYPOINT ["/account"]
