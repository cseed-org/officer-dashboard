//go:build linux

package setup

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"golang.org/x/term"

	"officerdashboard/internal/setup/discord"
)

// Review of #128, S-1, on a real terminal: the setup code is pasted together with a
// second line "yes", as text someone else supplied might be. That line must not answer
// the owner question; only what is typed after the question does. The test binary runs
// itself under script(1) for a pty, pastes both lines in one write, waits for the
// question, then types "no". Without the flush the queued "yes" answers first.
func TestOwnerQuestionIgnoresAPastedYesOnARealTerminal(t *testing.T) {
	if os.Getenv("SETUP_PTY_HELPER") == "1" {
		ptyOwnerHelper()
		return
	}
	script, err := exec.LookPath("script")
	if err != nil {
		t.Skip("needs script(1) (util-linux) for a pseudo-terminal")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	inner := "'" + strings.ReplaceAll(exe, "'", `'\''`) + "' -test.run='^TestOwnerQuestionIgnoresAPastedYesOnARealTerminal$'"
	cmd := exec.CommandContext(ctx, script, "-qec", inner, "/dev/null")
	cmd.Env = append(os.Environ(), "SETUP_PTY_HELPER=1")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var seen strings.Builder
	r := bufio.NewReader(stdout)
	waitFor := func(marker string) bool {
		for !strings.Contains(seen.String(), marker) {
			b, err := r.ReadByte()
			if err != nil {
				return false
			}
			seen.WriteByte(b)
		}
		return true
	}
	if !waitFor("Setup code:") {
		t.Fatalf("no paste prompt:\n%s", seen.String())
	}
	// One write, as a paste arrives: the code line and a "yes" line.
	if _, err := io.WriteString(stdin, "setup.AAAA:Zq9TqBy3mKcJ0dRw7LhVf2NsXe5Ua1\nyes\n"); err != nil {
		t.Fatal(err)
	}
	if !waitFor("Type yes: ") {
		t.Fatalf("no owner question:\n%s", seen.String())
	}
	if strings.Contains(seen.String(), "owner=") {
		t.Fatalf("answered before anything was typed:\n%s", seen.String())
	}
	if _, err := io.WriteString(stdin, "no\n"); err != nil {
		t.Fatal(err)
	}
	if !waitFor("owner=") {
		t.Fatalf("no answer read:\n%s", seen.String())
	}
	rest, _ := r.ReadString('\n') // the rest of the owner= line
	seen.WriteString(rest)
	stdin.Close()
	_ = cmd.Wait()
	out := seen.String()
	if !strings.Contains(out, "paste=\"setup.AAAA:Zq9TqBy3mKcJ0dRw7LhVf2NsXe5Ua1\"") {
		t.Errorf("the paste was not read as one line:\n%s", out)
	}
	if !strings.Contains(out, "owner=false") {
		t.Errorf("the pasted yes answered the owner question:\n%s", out)
	}
}

// ptyOwnerHelper runs in the pty: the same reads connect makes (a hidden paste, then
// confirmOwner with the real terminal and the real flush).
func ptyOwnerHelper() {
	fd := int(os.Stdin.Fd())
	fmt.Print("Setup code: ")
	raw, err := term.ReadPassword(fd)
	fmt.Println()
	if err != nil {
		fmt.Println("paste error:", err)
		os.Exit(1)
	}
	fmt.Printf("paste=%q\n", raw)
	deps := Deps{Stdout: os.Stdout, Stderr: os.Stdout, StdinFD: fd, IsTerminal: term.IsTerminal, Stdin: os.Stdin}
	ok := confirmOwner(deps, &discord.User{ID: "100000000000000042", Username: "admin", GlobalName: "Club Admin"})
	fmt.Printf("owner=%v\n", ok)
	os.Exit(0)
}
