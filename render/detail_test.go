package render

import (
	"strings"
	"testing"
	"time"

	"github.com/AgusRdz/probe/store"
)

func TestPrintDetailVariantsSection(t *testing.T) {
	t.Parallel()

	ep := store.Endpoint{
		ID:          1,
		Method:      "POST",
		PathPattern: "/api/login",
		Protocol:    "rest",
		Source:      "observed",
		CallCount:   5,
	}

	variants := []store.RequestVariant{
		{ID: 1, EndpointID: 1, Fingerprint: "auth:none|body:password,username", Label: "", CallCount: 3, FirstSeen: time.Now(), LastSeen: time.Now()},
		{ID: 2, EndpointID: 1, Fingerprint: "auth:none|body:token", Label: "sso", CallCount: 2, FirstSeen: time.Now(), LastSeen: time.Now()},
	}

	var buf strings.Builder
	PrintDetail(&buf, ep, nil, nil, DetailOptions{
		NoColor:  true,
		Variants: variants,
	})

	out := buf.String()

	if !strings.Contains(out, "Variants") {
		t.Error("expected 'Variants' heading in output")
	}
	if !strings.Contains(out, "body:password,username") {
		t.Error("expected first variant fingerprint in output")
	}
	if !strings.Contains(out, "sso") {
		t.Error("expected second variant label in output")
	}
}

func TestPrintDetailNoVariantsSectionWhenOne(t *testing.T) {
	t.Parallel()

	ep := store.Endpoint{
		ID:          1,
		Method:      "GET",
		PathPattern: "/api/users",
		Protocol:    "rest",
		Source:      "observed",
		CallCount:   10,
	}

	variants := []store.RequestVariant{
		{ID: 1, EndpointID: 1, Fingerprint: "auth:none|body:", CallCount: 10},
	}

	var buf strings.Builder
	PrintDetail(&buf, ep, nil, nil, DetailOptions{
		NoColor:  true,
		Variants: variants,
	})

	out := buf.String()
	if strings.Contains(out, "Variants") {
		t.Error("expected no 'Variants' section when there is only one variant")
	}
}
