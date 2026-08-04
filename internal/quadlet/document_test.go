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
