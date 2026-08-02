package command

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestConfirmActionRequiresCompleteLine(t *testing.T) {
	command := &cobra.Command{}
	command.SetIn(strings.NewReader("yes"))
	command.SetErr(&bytes.Buffer{})
	confirmed, err := confirmAction(command, "Continue?")
	if confirmed || !errors.Is(err, io.EOF) {
		t.Fatalf("confirmed=%t err=%v", confirmed, err)
	}
}

func TestConfirmActionAcceptsCompleteAffirmativeLine(t *testing.T) {
	command := &cobra.Command{}
	command.SetIn(strings.NewReader("yes\n"))
	command.SetErr(&bytes.Buffer{})
	confirmed, err := confirmAction(command, "Continue?")
	if err != nil || !confirmed {
		t.Fatalf("confirmed=%t err=%v", confirmed, err)
	}
}
