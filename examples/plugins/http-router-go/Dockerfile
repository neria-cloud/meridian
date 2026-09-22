FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/http-router-go .

FROM alpine:3.20
COPY --from=build /out/http-router-go /usr/local/bin/http-router-go
EXPOSE 18083
ENTRYPOINT ["/usr/local/bin/http-router-go"]
