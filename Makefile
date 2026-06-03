default: build

build:
	go build -o terraform-provider-medplum

fmt:
	gofmt -w .

vet:
	go vet ./...

test:
	go test ./... -count=1

testacc:
	TF_ACC=1 go test ./... -count=1 -v -timeout 30m

.PHONY: default build fmt vet test testacc
