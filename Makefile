GCFLAGS = -gcflags "all=-trimpath=$(PWD)" -asmflags "all=-trimpath=$(PWD)"

GO_BUILD_ENV_VARS := GO111MODULE=on CGO_ENABLED=0

all: build

build: clean
	$(GO_BUILD_ENV_VARS) go build -o bin/e2d $(GCFLAGS) ./cmd/e2d

test:
	go test ./...

test-manager:
	go test ./pkg/manager -test.long

clean:
	@rm -rf ./bin/*
