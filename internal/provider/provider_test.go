package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"temporalschedules": providerserver.NewProtocol6WithError(New("test")()),
}

func TestProviderNew(t *testing.T) {
	factory := New("test")
	p := factory()
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}
