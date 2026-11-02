package partner

import "testing"

func TestValidCustomFields(t *testing.T) {
	fields := []XMLField{
		{Name: "provider", Value: "a"},
		{Name: "id", Value: "collision"},
		{Name: "title", Source: "programme.name", When: "metadata"},
		{Name: "provider", Value: "dup"},
	}
	valid, errs := ValidCustomFields(fields)
	if len(valid) != 2 {
		t.Fatalf("expected 2 valid fields, got %d: %+v", len(valid), valid)
	}
	if len(errs) != 2 {
		t.Fatalf("expected 2 errors, got %d: %v", len(errs), errs)
	}
	if valid[0].Name != "provider" || valid[1].Name != "title" {
		t.Fatalf("unexpected valid order: %+v", valid)
	}
}

func TestCustomFieldReservedNames(t *testing.T) {
	reserved := []string{"id", "name", "img", "start", "stop", "streamId", "ads", "channelId"}
	for _, name := range reserved {
		f := XMLField{Name: name, Value: "x"}
		if err := f.Validate(); err == nil {
			t.Fatalf("expected %q to be rejected as reserved", name)
		}
	}
}

func TestCustomFieldExactlyOneOfSourceValue(t *testing.T) {
	if err := (XMLField{Name: "x", Source: "a"}).Validate(); err != nil {
		t.Fatalf("source-only should be valid: %v", err)
	}
	if err := (XMLField{Name: "x", Value: "b"}).Validate(); err != nil {
		t.Fatalf("value-only should be valid: %v", err)
	}
	if err := (XMLField{Name: "x", Source: "a", Value: "b"}).Validate(); err == nil {
		t.Fatal("both source and value should be invalid")
	}
	if err := (XMLField{Name: "x"}).Validate(); err == nil {
		t.Fatal("neither source nor value should be invalid")
	}
}

func TestCustomFieldWhenValues(t *testing.T) {
	for _, when := range []string{"", "always", "metadata"} {
		if err := (XMLField{Name: "x", Value: "y", When: when}).Validate(); err != nil {
			t.Fatalf("when %q should be valid: %v", when, err)
		}
	}
	if err := (XMLField{Name: "x", Value: "y", When: "occasionally"}).Validate(); err == nil {
		t.Fatal("unexpected when value should be invalid")
	}
}
