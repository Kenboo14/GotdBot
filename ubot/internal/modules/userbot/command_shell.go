package userbot

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	gobot "github.com/Kenboo14/GoBot"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/telegram/message/styling"
	"github.com/gotd/td/tg"
)

// shellCommand handles the userbot !sh command.
func (s *Service) shellCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, prefix, args string, threadID int) error {
	err := sendUserText(ctx, client, target, "Fitur !sh dinonaktifkan untuk menghemat RAM.", threadID)
	return err
}

func (s *Service) runCommand(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, prefix, name, args string, threadID int) error {
	if strings.TrimSpace(args) == "" {
		err := sendUserText(ctx, client, target, "Gunakan: "+prefix+name+" <perintah>", threadID)
		return err
	}
	output, runErr := processCommand(ctx, args)
	if runErr != nil {
		output = runErr.Error() + "\n" + output
	}
	return s.sendCommandOutput(ctx, client, target, output, threadID)
}

// processCommand is the common executor used by both !eval and !sh.
func processCommand(ctx context.Context, command string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
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

func (s *Service) sendCommandOutput(ctx context.Context, client *gobot.Client, target tg.InputPeerClass, output string, threadID int) error {
	if strings.TrimSpace(output) == "" {
		output = "(tidak ada output)"
	}
	if threadID > 0 {
		err := sendUserText(ctx, client, target, output, threadID)
		return err
	}
	sender := client.Sender()
	if len(output) > 3900 {
		_, err := sender.To(target).Upload(message.FromBytes("dump.txt", []byte(output))).File(ctx)
		return err
	}
	_, err := sender.To(target).StyledText(ctx, styling.Code(output))
	return err
}
