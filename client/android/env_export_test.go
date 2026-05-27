package android

import (
	"os"
	"testing"
)

func TestExportEnvList(t *testing.T) {
	t.Setenv("NB_TEST_PROXY_ENV", "before")

	exportEnvList(nil)
	if got := os.Getenv("NB_TEST_PROXY_ENV"); got != "before" {
		t.Fatalf("nil env list changed environment: got %q", got)
	}

	envList := NewEnvList()
	envList.Put("NB_TEST_PROXY_ENV", "after")

	exportEnvList(envList)
	if got := os.Getenv("NB_TEST_PROXY_ENV"); got != "after" {
		t.Fatalf("env list was not exported: got %q", got)
	}
}
