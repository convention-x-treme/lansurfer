FROM golang:1.27.1-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY tools/loadtest/proxy ./tools/loadtest/proxy
RUN CGO_ENABLED=0 go build -o /proxy ./tools/loadtest/proxy

FROM golang:1.27.1-alpine
RUN apk add --no-cache iproute2
COPY --from=build /proxy /proxy
ENTRYPOINT ["/proxy"]
