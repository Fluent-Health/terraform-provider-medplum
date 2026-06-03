package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/Fluent-Health/terraform-provider-medplum/internal/provider"
)

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
