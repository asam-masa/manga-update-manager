package restoreswitch

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const recoveryProcessFixture = "synthetic-recovery-process-fixture"

func TestRecoveryAfterSwitchProcessKilled(t *testing.T) {
	root := canonicalRecoveryTestDirectory(t, recoveryFixture(t, 0))
	write(t, root, recoveryProcessFixture, recoveryProcessFixture)
	if err := os.Mkdir(filepath.Join(root, "records"), 0700); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	switcher := recoveryProcessCommand(t, ctx, root, "place-image")
	output, err := switcher.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := switcher.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	switcher.Stderr = os.Stderr
	if err := switcher.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if switcher.ProcessState == nil {
			_ = switcher.Process.Kill()
			_ = switcher.Wait()
		}
	}()
	line, err := bufio.NewReader(output).ReadString('\n')
	if err != nil || line != "new-image-placed\n" {
		t.Fatal("switch child did not reach the checkpoint", line, err)
	}
	if err := switcher.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	var exitErr *exec.ExitError
	if err := switcher.Wait(); !errors.As(err, &exitErr) || exitErr.Success() || ctx.Err() != nil {
		t.Fatal("expected forced child termination", err, ctx.Err())
	}
	content(t, root, liveImage, "new-image")
	content(t, root, retiredDB, "old-db")
	content(t, root, retiredImage, "old-image")
	content(t, root, stagedDB, "new-db")
	if _, err := os.Lstat(filepath.Join(root, liveDB)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("expected no live DB during partial switch", err)
	}
	if _, err := os.Lstat(filepath.Join(root, completeName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("expected no completion record", err)
	}

	recovery := recoveryProcessCommand(t, ctx, root, "recover")
	if output, err := recovery.CombinedOutput(); err != nil {
		t.Fatal(string(output), err)
	}
	content(t, root, liveDB, "old-db")
	content(t, root, liveImage, "old-image")
	content(t, root, stagedDB, "new-db")
	content(t, root, stagedImage, "new-image")
}

func recoveryProcessCommand(t *testing.T, ctx context.Context, root, mode string) *exec.Cmd {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestRecoveryProcessHelper$")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "MANGA_TEST_RECOVERY_PROCESS="+mode)
	return cmd
}

func TestRecoveryProcessHelper(t *testing.T) {
	mode := os.Getenv("MANGA_TEST_RECOVERY_PROCESS")
	if mode == "" {
		return
	}
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	content(t, root, recoveryProcessFixture, recoveryProcessFixture)
	switch mode {
	case "place-image":
		plan, err := json.Marshal(recoveryFixturePlan)
		if err != nil {
			t.Fatal(err)
		}
		publishRecoveryRecord(t, prepareRecoveryRecord(t, root, planName, plan))
		move(t, root, liveDB, retiredDB)
		move(t, root, liveImage, retiredImage)
		move(t, root, stagedImage, liveImage)
		if _, err := fmt.Fprintln(os.Stdout, "new-image-placed"); err != nil {
			t.Fatal(err)
		}
		_, err = bufio.NewReader(os.Stdin).ReadByte()
		t.Fatal("switch child was released instead of killed", err)
	case "recover":
		for i := 0; i <= 4; i++ {
			outcome, moves, err := decideRecovery(root)
			if outcome != "restore old" || err != nil {
				t.Fatal(outcome, moves, err)
			}
			if len(moves) == 0 {
				return
			}
			move(t, root, moves[0].from, moves[0].to)
		}
		t.Fatal("recovery did not converge")
	default:
		t.Fatal("unknown recovery fixture mode")
	}
}
