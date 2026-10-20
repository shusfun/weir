package control

import "testing"

func TestModelEndpointNormalization(t *testing.T) {
	for _, raw := range []string{"https://model.example", "https://model.example/", "https://model.example/v1/systemone", "https://model.example/v1/systemone/"} {
		u, err := modelEndpoint(raw)
		if err != nil || u.String() != "https://model.example/v1/systemone" {
			t.Fatalf("endpoint not normalized: %s", raw)
		}
	}
	for _, raw := range []string{"http://model.example", "https://u:p@model.example", "https://model.example?key=secret", "https://model.example#secret", "https://model.example/%2fprivate", "https:///", "https://model.example\n"} {
		if _, err := modelEndpoint(raw); err == nil {
			t.Fatal("unsafe endpoint accepted")
		}
	}
}
