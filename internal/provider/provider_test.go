package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// testAccProtoV6ProviderFactories is used by acceptance tests.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"medplum": providerserver.NewProtocol6WithError(New("test")()),
}

func TestProvider_ImplementsInterface(t *testing.T) {
	var _ = New("test")()
}

func TestValidateBotRuntimes(t *testing.T) {
	if err := validateBotRuntimes([]string{"vmcontext", "fission"}); err != nil {
		t.Fatalf("valid runtimes rejected: %v", err)
	}
	if err := validateBotRuntimes([]string{"vmcontext", "docker"}); err == nil {
		t.Fatal("expected error for unknown runtime name")
	}
}
