package request

import (
	"context"
	"os"
	"os/exec"
	"testing"
)

func TestWindowsCancellationBeforeJobAssignment(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(context.Background(), exe)
	owner, err := ownProcess(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.close()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// The process is suspended and must be killed even though the job is empty.
	if err := owner.kill(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("cancelled suspended process succeeded")
	}
	if err := owner.afterStart(); err == nil {
		t.Fatal("assigned an exited process")
	}
}

func TestWindowsResumeMissingProcess(t *testing.T) {
	if err := resumeProcess(^uint32(0)); err == nil {
		t.Fatal("resumed a nonexistent process")
	}
}
