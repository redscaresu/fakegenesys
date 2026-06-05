# syntax=docker/dockerfile:1.6

FROM golang:1.25 AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags '-s -w' -o /out/fakegenesys ./cmd/fakegenesys

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/fakegenesys /usr/local/bin/fakegenesys
EXPOSE 8083
ENTRYPOINT ["/usr/local/bin/fakegenesys"]
CMD ["--port", "8083"]
