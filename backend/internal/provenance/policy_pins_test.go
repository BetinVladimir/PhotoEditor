package provenance

import "testing"

func TestInstalledPolicyPinsOwnExactVersion(t *testing.T) {
	config, params, _ := signingRegistryFixture(t)
	registry, err := NewSigningPolicyRegistry(config, &signingAuthorityStub{})
	if err != nil {
		t.Fatal(err)
	}
	pins, err := registry.InstalledPolicyPins()
	if err != nil || len(pins) != 1 || pins[0].PolicyID != params.PolicyID || pins[0].PolicySHA != params.Policy.SHA256 {
		t.Fatal("installed policy pin drift", pins, err)
	}
	config.Policies[0].Ref.SHA256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	again, err := registry.InstalledPolicyPins()
	if err != nil || again[0] != pins[0] {
		t.Fatal("registry borrowed caller policy", err)
	}
}
