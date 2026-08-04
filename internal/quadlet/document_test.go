package quadlet

import "testing"

func TestRender(t *testing.T) {
	t.Parallel()

	actual := string(Render(
		Section{Name: "Unit", Pairs: []Pair{{Key: "Description", Value: "Example"}}},
		Section{Name: "Network", Pairs: []Pair{{Key: "Driver", Value: "bridge"}}},
	))
	expected := "# Managed by terraform-provider-podlet\n# Format: 1\n\n" +
		"[Unit]\nDescription=Example\n\n[Network]\nDriver=bridge\n"
	if actual != expected {
		t.Fatalf("unexpected document:\n%s", actual)
	}
	if !Managed([]byte(actual)) {
		t.Fatal("rendered document was not recognized as managed")
	}
}

func TestManagedRejectsOtherContent(t *testing.T) {
	t.Parallel()

	if Managed([]byte("[Container]\nImage=example")) {
		t.Fatal("unmarked document was recognized as managed")
	}
}

func TestParsePreservesRepeatedKeys(t *testing.T) {
	t.Parallel()

	sections, err := Parse(Render(Section{
		Name: "Container",
		Pairs: []Pair{
			{Key: "Environment", Value: "A=one"},
			{Key: "Environment", Value: "B=two"},
		},
	}))
	if err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}
	if len(sections["Container"]) != 2 {
		t.Fatalf("expected two entries, got %#v", sections["Container"])
	}
}
