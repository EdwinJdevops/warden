BINARY_NAME := warden
BUILD_DIR := bin
CMD_PATH := ./controller/cmd/warden

.PHONY: build test fmt vet clean install

build:
	mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY_NAME) $(CMD_PATH)

test:
	go test ./controller/... -v

fmt:
	gofmt -l .

vet:
	go vet ./...

clean:
	rm -rf $(BUILD_DIR)

install:
	go install $(CMD_PATH)
