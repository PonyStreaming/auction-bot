FROM golang:1.14.2-alpine as build

WORKDIR /go/src/app
ADD . /go/src/app
RUN CGO_ENABLED=0 GOOS=linux go build -o /go/bin/app

FROM gcr.io/distroless/static
COPY --from=build /go/bin/app /
ENTRYPOINT ["/app"]
