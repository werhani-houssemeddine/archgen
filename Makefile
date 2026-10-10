.PHONY: build install test vet

build:
	go build -o bin/archgen ./cmd/archgen

install:
	go install ./cmd/archgen

vet:
	go vet ./...

# Set ARCHKIT_PATH to a local archkit checkout to also compile & test a generated project.
test: vet
	go test -count=1 ./...
