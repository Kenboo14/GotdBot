package userbot

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/telegram/message/styling"
	"github.com/gotd/td/tg"
)

// shellCommand handles the userbot !sh command.
func (s *Service) shellCommand(ctx context.Context, client *telegram.Client, target tg.InputPeerClass, prefix, args string) error {
	return s.runCommand(ctx, client, target, prefix, "sh", args)
}

func (s *Service) runCommand(ctx context.Context, client *telegram.Client, target tg.InputPeerClass, prefix, name, args string) error {
	if strings.TrimSpace(args) == "" {
		_, err := message.NewSender(tg.NewClient(client)).To(target).Text(ctx, "Gunakan: "+prefix+name+" <perintah>")
		return err
	}
	output, runErr := processCommand(ctx, args)
	if runErr != nil {
		output = runErr.Error() + "\n" + output
	}
	return sendCommandOutput(ctx, client, target, output)
}

// processCommand is the common executor used by both !eval and !sh.
func processCommand(ctx context.Context, command string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	shell, args := "sh", []string{"-c", command}
	if runtime.GOOS == "windows" {
		// PowerShell supports common shell commands such as ls, pwd and cat,
		// while still allowing native Windows commands.
		shell, args = "powershell.exe", []string{"-NoProfile", "-NonInteractive", "-Command", command}
	}
	out, err := exec.CommandContext(ctx, shell, args...).CombinedOutput()
	if ctx.Err() != nil {
		return string(out), fmt.Errorf("perintah dihentikan karena timeout")
	}
	return string(out), err
}

func sendCommandOutput(ctx context.Context, client *telegram.Client, target tg.InputPeerClass, output string) error {
	if strings.TrimSpace(output) == "" {
		output = "(tidak ada output)"
	}
	if len(output) > 3900 {
		_, err := message.NewSender(tg.NewClient(client)).To(target).Upload(message.FromBytes("dump.txt", []byte(output))).File(ctx)
		return err
	}
	_, err := message.NewSender(tg.NewClient(client)).To(target).StyledText(ctx, styling.Code(output))
	return err
}
