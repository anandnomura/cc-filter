package resourceindicator

import "testing"

func TestStrictResourceIndicator(t *testing.T) {
	if err := Validate("https://orders.company.example/api/"); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{
		"", "orders", "http://orders.company.example/", "https://orders.company.example/api?tenant=one",
		"https://orders.company.example/api#fragment", "https://user@orders.company.example/", " https://orders.company.example/",
	} {
		if err := Validate(value); err == nil {
			t.Fatalf("invalid resource indicator %q was accepted", value)
		}
	}
}
