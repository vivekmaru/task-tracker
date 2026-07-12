//go:build integration

package integration_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/vivek/agent-task-tracker/internal/testsupport"
)

func TestMain(m *testing.M) {
	if _, err := testsupport.TestDatabaseURL(); err != nil {
		fmt.Fprintf(os.Stderr, "integration configuration error: %v\n", err)
		os.Exit(2)
	}
	os.Exit(m.Run())
}
