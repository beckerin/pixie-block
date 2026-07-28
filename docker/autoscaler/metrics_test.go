package main

import "testing"

func TestSumTraefikServiceRequests(t *testing.T) {
	body := []byte(`# HELP traefik_service_requests_total
traefik_service_requests_total{code="200",method="GET",protocol="http",service="pixie-api@docker"} 10
traefik_service_requests_total{code="200",method="POST",protocol="http",service="pixie-api@docker"} 5
traefik_service_requests_total{code="200",method="GET",protocol="http",service="other@docker"} 100
`)
	total, err := sumTraefikServiceRequests(body, "pixie-api")
	if err != nil {
		t.Fatal(err)
	}
	if total != 15 {
		t.Fatalf("got %v want 15", total)
	}
}
