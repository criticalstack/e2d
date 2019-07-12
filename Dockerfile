FROM golang:1.12-alpine AS go-builder

# Create the user and group files that will be used in the running container to
# run the process as an unprivileged user.
RUN mkdir /user && \
    echo 'nobody:x:65534:65534:nobody:/:' > /user/passwd && \
    echo 'nobody:x:65534:' > /user/group

RUN apk add --no-cache ca-certificates git

# Set the environment variables for the go command
ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on

WORKDIR /e2d

# Copy Go source code files, and built static assets from previous stages
# Start with vendor since it doesn't change often, to utilize caching
COPY ./go.mod ./go.sum ./
COPY pkg ./pkg
COPY cmd ./cmd

# Put these right before the go build since they will change with each commit,
# which reduces docker caching
ARG VERSION=dev

RUN go build \
    -ldflags "-s -w -X main.Version=$VERSION" \
    -o bin/e2d \
    ./cmd/e2d

RUN chown nobody:nobody bin/e2d

############################
# Final stage: Just the executable and bare minimum other files
FROM scratch AS final

LABEL MAINTAINER="Critical Stack <dev@criticalstack.com>"

# Import the user and group files from the first stage.
COPY --from=go-builder /user/group /user/passwd /etc/

# Import the Certificate-Authority certificates for enabling HTTPS.
COPY --from=go-builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Perform any further action as an unprivileged user.
USER nobody:nobody

# e2d runs on port 2379,2380,7980
EXPOSE 2379
EXPOSE 2380
EXPOSE 7980

# Add e2d bin
COPY --from=go-builder --chown=nobody:nobody /e2d/bin/e2d /

ENTRYPOINT ["/e2d"]