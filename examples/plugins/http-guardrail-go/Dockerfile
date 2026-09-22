FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/http-guardrail-go .

FROM alpine:3.20
COPY --from=build /out/http-guardrail-go /usr/local/bin/http-guardrail-go
EXPOSE 18082
ENTRYPOINT ["/usr/local/bin/http-guardrail-go"]
