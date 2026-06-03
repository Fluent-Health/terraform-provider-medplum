package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/Fluent-Health/terraform-provider-medplum/internal/provider"
)

// Run "go generate" to format example terraform files and generate the docs for the registry/website.

// If you do not have terraform installed, you can remove the formatting command, but it is suggested to
// ensure the documentation is formatted properly.
//go:generate terraform fmt -recursive ./examples/

// Run the docs generation tool, check its repository for more information on how it works and how docs
// can be customized.
//go:generate go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate -provider-name medplum

// version is set by the release build via -ldflags.
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "set to run the provider with debugger support")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/Fluent-Health/medplum",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err.Error())
	}
}
