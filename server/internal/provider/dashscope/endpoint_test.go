package dashscope

import (
	"errors"
	"testing"

	"github.com/chenhao/omnigen-ai/server/internal/pkg/apperr"
)

func TestResolveRegionURL_KnownRegions(t *testing.T) {
	cases := []struct {
		region string
		want   string
	}{
		{"cn-beijing", "https://dashscope.aliyuncs.com"},
		{"ap-southeast-1", "https://dashscope-intl.aliyuncs.com"},
		{"us-east-1", "https://dashscope-us.aliyuncs.com"},
	}
	for _, tc := range cases {
		t.Run(tc.region, func(t *testing.T) {
			got, err := resolveRegionURL(tc.region, "")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("resolveRegionURL(%q) = %q, want %q", tc.region, got, tc.want)
			}
		})
	}
}

func TestResolveRegionURL_EuCentralTemplatesWorkspaceID(t *testing.T) {
	got, err := resolveRegionURL("eu-central-1", "ws-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://ws-123.eu-central-1.maas.aliyuncs.com"
	if got != want {
		t.Errorf("resolveRegionURL(eu-central-1) = %q, want %q", got, want)
	}
}

func TestResolveRegionURL_EuCentralWithoutWorkspaceIDIsValidationError(t *testing.T) {
	_, err := resolveRegionURL("eu-central-1", "")
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !errors.Is(err, apperr.ErrValidation) {
		t.Errorf("expected apperr.ErrValidation, got %v (%T)", err, err)
	}

	var appErr *apperr.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *apperr.AppError, got %T", err)
	}
	if appErr.HTTPStatus() != 422 {
		t.Errorf("expected HTTP 422, got %d", appErr.HTTPStatus())
	}
}

func TestResolveRegionURL_UnknownRegionFallsBackToBeijing(t *testing.T) {
	for _, region := range []string{"", "mars-1", "unknown-region"} {
		got, err := resolveRegionURL(region, "")
		if err != nil {
			t.Fatalf("region=%q: unexpected error: %v", region, err)
		}
		if got != "https://dashscope.aliyuncs.com" {
			t.Errorf("region=%q: resolveRegionURL = %q, want Beijing fallback", region, got)
		}
	}
}

func TestClientBaseURL_ExplicitEndpointOverridesRegion(t *testing.T) {
	c := New("key", "cn-beijing", "", "https://custom.example.com/")
	got, err := c.baseURL()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "https://custom.example.com" {
		t.Errorf("baseURL = %q, want trailing slash stripped", got)
	}
}

func TestClientBaseURL_NoEndpointUsesRegion(t *testing.T) {
	c := New("key", "us-east-1", "", "")
	got, err := c.baseURL()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "https://dashscope-us.aliyuncs.com" {
		t.Errorf("baseURL = %q, want us-east-1 endpoint", got)
	}
}
