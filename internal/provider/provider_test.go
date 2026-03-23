package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){ //nolint:unused // used by acceptance tests in Phase 2
	"temporalschedules": providerserver.NewProtocol6WithError(New("test")()),
}

func TestProviderNew(t *testing.T) {
	factory := New("test")
	p := factory()
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}
