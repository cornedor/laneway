.PHONY: build test run install

build:
	go build -o laneway .

test:
	go test ./...

run: build
	./laneway

install:
	go install .
