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

doc:
	GOTOOLCHAIN=go1.25.8 ASDF_TERRAFORM_VERSION=1.12.1 go generate ./...

.PHONY: default build fmt vet test testacc doc
