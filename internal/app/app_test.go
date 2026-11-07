package app

import (
	"bytes"
	"errors"
	"net/http"
	"strings"
	"testing"
)

type networkProbe func(*http.Request) (*http.Response, error)

func (probe networkProbe) RoundTrip(request *http.Request) (*http.Response, error) {
	return probe(request)
}

func TestRetiredCommandsCannotRequestNetwork(t *testing.T) {
	original := http.DefaultTransport
	requests := 0
	http.DefaultTransport = networkProbe(func(*http.Request) (*http.Response, error) {
		requests++
		return nil, errors.New("unexpected network request")
	})
	defer func() { http.DefaultTransport = original }()
	for _, args := range [][]string{
		nil, {"--wait"}, {"--commit", strings.Repeat("a", 40), "--branch", "main"},
		{"_worker", "existing-operation"}, {"retry", "existing-operation"},
		{"wait", "existing-operation"}, {"status", "existing-operation"},
		{"observe", "existing-operation"}, {"logs", "existing-operation"},
		{"check"}, {"install"}, {"install", "--skills-only"}, {"skill"}, {"update"},
		{"version", "unexpected-argument"},
	} {
		var output, diagnostics bytes.Buffer
		if err := Run(args, "test", &output, &diagnostics); !errors.Is(err, ErrRetired) {
			t.Fatalf("Run(%q) error = %v; want retirement", args, err)
		}
		if output.Len() != 0 || diagnostics.Len() != 0 {
			t.Fatalf("Run(%q) emitted unexpected output", args)
		}
	}
	if requests != 0 {
		t.Fatalf("retired commands made %d network requests", requests)
	}
}

func TestInformationalCommands(t *testing.T) {
	for _, command := range []string{"help", "-h", "--help", "version", "--version"} {
		var output bytes.Buffer
		if err := Run([]string{command}, "test-version", &output, &output); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(command, "version") {
			if output.String() != "test-version\n" {
				t.Fatalf("version = %q", output.String())
			}
		} else if !strings.Contains(output.String(), "retired") || !strings.Contains(output.String(), "Jenkins") {
			t.Fatalf("help = %q", output.String())
		}
	}
}
