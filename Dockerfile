# syntax=docker/dockerfile:1

FROM golang:1.27.1-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/lansurfer ./cmd/lansurfer
# Empty cover cache directory; a new named volume mounted here inherits its owner.
RUN mkdir -p /out/covers

FROM scratch

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/lansurfer /lansurfer
COPY --from=build --chown=65532:65532 /out/covers /var/cache/lansurfer/covers

USER 65532:65532
EXPOSE 8080

ENTRYPOINT ["/lansurfer"]
