FROM golang:1.27.2-alpine3.24 AS build
RUN apk add --no-cache gcc musl-dev ca-certificates
WORKDIR /src
COPY go.* ./
RUN go mod download
COPY main.go ./
RUN go mod tidy && CGO_ENABLED=1 go build -tags netgo,osusergo -trimpath \
    -ldflags='-s -w -linkmode external -extldflags "-static"' \
    -o /wa-up .

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /wa-up /wa-up
USER 62978:62978
WORKDIR /data
ENTRYPOINT ["/wa-up"]
