# Build from the repo root: docker build -f deploy/Dockerfile .
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY api ./api
COPY cmd ./cmd
COPY internal ./internal
COPY pkg ./pkg
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/app-http ./cmd/app-http \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/healthcheck ./cmd/healthcheck

FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/app-http /out/healthcheck /usr/local/bin/
COPY files/etc/bookcabin-assignment/development/config.yaml /etc/bookcabin-assignment/development/config.yaml
COPY files/etc/bookcabin-assignment/staging/config.yaml /etc/bookcabin-assignment/staging/config.yaml
COPY files/etc/bookcabin-assignment/production/config.yaml /etc/bookcabin-assignment/production/config.yaml
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/app-http"]
