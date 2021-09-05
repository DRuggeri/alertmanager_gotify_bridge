#NOTE: This Dockerfile is intended for those that would like to locally build their
# own image. Official images, built via CI/CD pipeline during release, are available
# in the README
FROM golang:1.17.0-alpine3.14 AS builder

RUN apk --no-cache add git

ARG VERSION
RUN go install github.com/DRuggeri/alertmanager_gotify_bridge@$VERSION

FROM alpine:3.14
COPY --from=builder /go/bin/alertmanager_gotify_bridge /usr/bin/alertmanager_gotify_bridge

ENTRYPOINT ["/usr/bin/alertmanager_gotify_bridge"]
