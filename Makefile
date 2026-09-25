.PHONY: build test run install

build:
	go build -o jiratui .

test:
	go test ./...

run: build
	./jiratui

install:
	go install .
