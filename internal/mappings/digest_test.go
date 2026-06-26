package mappings

import "testing"

const digestMappingsTail = `
mappings:
  acme:
    "*":
      channel: C0123ABCDE
`

func TestProvider_Digest_AbsentDefaultsToEnabled(t *testing.T) {
	p, err := Load(writeMappingsFile(t, digestMappingsTail))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	d := p.Digest()
	if !d.Enabled {
		t.Errorf("digest disabled with no section; want enabled by default")
	}
	if d.Schedule != DefaultDigestSchedule {
		t.Errorf("schedule = %q; want default %q", d.Schedule, DefaultDigestSchedule)
	}
}

func TestProvider_Digest_CustomSchedule(t *testing.T) {
	body := "digest:\n  schedule: \"0 8 * * 1-5\"\n" + digestMappingsTail
	p, err := Load(writeMappingsFile(t, body))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	d := p.Digest()
	if !d.Enabled {
		t.Errorf("want enabled")
	}
	if d.Schedule != "0 8 * * 1-5" {
		t.Errorf("schedule = %q; want custom", d.Schedule)
	}
}

func TestProvider_Digest_ExplicitlyDisabled(t *testing.T) {
	body := "digest:\n  enabled: false\n" + digestMappingsTail
	p, err := Load(writeMappingsFile(t, body))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	d := p.Digest()
	if d.Enabled {
		t.Errorf("digest enabled despite `enabled: false`")
	}
	// The schedule still resolves to the default even while disabled.
	if d.Schedule != DefaultDigestSchedule {
		t.Errorf("schedule = %q; want default", d.Schedule)
	}
}

func TestProvider_Digest_UnknownFieldRejected(t *testing.T) {
	body := "digest:\n  frequency: daily\n" + digestMappingsTail
	if _, err := Load(writeMappingsFile(t, body)); err == nil {
		t.Fatalf("expected parse error for unknown digest field, got nil")
	}
}
