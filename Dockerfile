FROM golang:1.24-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/claw ./cmd/claw

FROM gcr.io/distroless/static-debian12

WORKDIR /

COPY --from=build /out/claw /usr/local/bin/claw
COPY configs/config.example.yaml /etc/claw/config.yaml

EXPOSE 8080

USER nonroot:nonroot

ENTRYPOINT ["/usr/local/bin/claw"]
CMD ["-config", "/etc/claw/config.yaml"]
