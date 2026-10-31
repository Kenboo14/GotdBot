package userbot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/telegram/message/styling"
	"github.com/gotd/td/tg"
	"github.com/redis/go-redis/v9"
	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
	"go.mongodb.org/mongo-driver/mongo"
)

const (
	maxEvalCodeBytes   = 16 * 1024
	maxEvalOutputBytes = 64 * 1024
)

// evaluatorCommand evaluates Go code. Shell commands remain exclusively in
// command_shell.go.
func (s *Service) evaluatorCommand(ctx context.Context, client *telegram.Client, target tg.InputPeerClass, msg *tg.Message, entities tg.Entities, prefix, args string) error {
	commandArgs := args
	var reply *tg.Message
	if msg != nil {
		reply, _ = s.replyMessage(ctx, client, msg, target)
	}
	if strings.TrimSpace(args) == "" && reply != nil {
		args = reply.Message
	}
	if strings.TrimSpace(args) == "" {
		_, err := message.NewSender(tg.NewClient(client)).To(target).Text(ctx, "Gunakan: "+prefix+"eval <kode Go>")
		return err
	}
	if len(args) > maxEvalCodeBytes {
		_, err := message.NewSender(tg.NewClient(client)).To(target).Text(ctx, fmt.Sprintf("Kode eval terlalu panjang (maksimal %d KB).", maxEvalCodeBytes/1024))
		return err
	}
	replyFunc := func(replyCtx context.Context, text string) error {
		// Use the command context even when Yaegi calls m.Reply(), so a reply
		// cannot outlive the eval timeout.
		if replyCtx == nil {
			replyCtx = ctx
		}
		_, err := message.NewSender(tg.NewClient(client)).To(target).Text(replyCtx, text)
		return err
	}
	output, err := evaluateGo(ctx, args, commandArgs, s, client, msg, reply, entities, replyFunc)
	if err != nil {
		output = err.Error() + "\n" + output
	}
	result := args + "\n\nOutput:\n" + output
	if len(result) > 3900 {
		_, sendErr := message.NewSender(tg.NewClient(client)).To(target).Upload(message.FromBytes("dump.txt", []byte(result))).File(ctx)
		return sendErr
	}
	_, err = message.NewSender(tg.NewClient(client)).To(target).StyledText(ctx,
		styling.Bold("Go\n"),
		styling.Pre(args, "go"),
		styling.Bold("\n\nOutput:\n"),
		styling.Pre(output, "go"),
	)
	return err
}

func evaluateGo(ctx context.Context, code, commandArgs string, service *Service, client *telegram.Client, msg, reply *tg.Message, entities tg.Entities, replyFunc func(context.Context, string) error) (output string, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			output = ""
			err = fmt.Errorf("panic saat evaluasi Go: %v", recovered)
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if service == nil || client == nil {
		return "", fmt.Errorf("context evaluator tidak tersedia")
	}
	var stdout, stderr limitedBuffer
	i := interp.New(interp.Options{Stdout: &stdout, Stderr: &stderr})
	if err := i.Use(stdlib.Symbols); err != nil {
		return "", err
	}
	exports := map[string]reflect.Value{
		"U":      reflect.ValueOf(service),
		"C":      reflect.ValueOf(client),
		"M":      reflect.ValueOf(newEvalMessage(msg, reply, entities, replyFunc, commandArgs)),
		"Replit": reflect.ValueOf(newEvalMessage(reply, nil, entities, replyFunc)),
		"DB":     reflect.ValueOf((*mongo.Database)(nil)),
		"R":      reflect.ValueOf((*redis.Client)(nil)),
	}
	if service.redis != nil && service.redis.Client != nil {
		exports["R"] = reflect.ValueOf(service.redis.Client)
	}
	if err := i.Use(interp.Exports{"gotdbotctx/gotdbotctx": exports}); err != nil {
		return "", err
	}
	if _, err := i.Eval(`import "gotdbotctx"`); err != nil {
		return "", err
	}
	bindings := `u := gotdbotctx.U; c := gotdbotctx.C; m := gotdbotctx.M; replit := gotdbotctx.Replit; reply := gotdbotctx.Replit`
	bindings += `; db := gotdbotctx.DB; r := gotdbotctx.R`
	if _, err := i.Eval(bindings); err != nil {
		return "", err
	}
	code, importDecls := splitEvalImports(code)
	// Keep common examples concise (fmt.Println, strings.TrimSpace, ...),
	// while avoiding duplicate declarations when the user imports explicitly.
	for _, item := range []struct{ name, path string }{
		{name: "fmt", path: "fmt"}, {name: "strings", path: "strings"},
		{name: "time", path: "time"}, {name: "json", path: "encoding/json"},
	} {
		if strings.Contains(code, item.name+".") && !evalImportsContain(importDecls, item.path) {
			if _, err := i.Eval(`import "` + item.path + `"`); err != nil {
				return "", err
			}
		}
	}
	for _, declaration := range importDecls {
		if _, err := i.Eval(declaration); err != nil {
			return "", err
		}
	}
	value, err := i.EvalWithContext(ctx, code)
	if err != nil {
		return strings.TrimSpace(stdout.String() + "\n" + stderr.String()), err
	}
	output = strings.TrimSpace(stdout.String())
	if stderr.Len() > 0 {
		output = strings.TrimSpace(output + "\n" + stderr.String())
	}
	if value.IsValid() && output == "" {
		actual := value.Interface()
		if actual == nil {
			return output, nil
		}
		rv := reflect.ValueOf(actual)
		valueText := fmt.Sprint(actual)
		if rv.IsValid() && (rv.Kind() == reflect.Struct || rv.Kind() == reflect.Map || rv.Kind() == reflect.Slice || (rv.Kind() == reflect.Ptr && !rv.IsNil() && rv.Elem().Kind() == reflect.Struct)) {
			if encoded, encodeErr := json.MarshalIndent(actual, "", "  "); encodeErr == nil {
				valueText = string(encoded)
			}
		}
		if valueText != "" && valueText != output {
			output = strings.TrimSpace(output + "\n" + valueText)
		}
	}
	return output, nil
}

func evalImportsContain(declarations []string, path string) bool {
	needle := `"` + path + `"`
	for _, declaration := range declarations {
		if strings.Contains(declaration, needle) {
			return true
		}
	}
	return false
}

// splitEvalImports lets callers paste normal Go snippets containing import
// declarations. Yaegi evaluates declarations and expressions separately.
func splitEvalImports(code string) (string, []string) {
	lines := strings.Split(code, "\n")
	kept := make([]string, 0, len(lines))
	imports := make([]string, 0)
	for index := 0; index < len(lines); index++ {
		trimmed := strings.TrimSpace(lines[index])
		if strings.HasPrefix(trimmed, "import (") {
			block := []string{trimmed}
			for index++; index < len(lines); index++ {
				block = append(block, lines[index])
				if strings.TrimSpace(lines[index]) == ")" {
					break
				}
			}
			imports = append(imports, strings.Join(block, "\n"))
			continue
		}
		if strings.HasPrefix(trimmed, "import ") {
			imports = append(imports, trimmed)
			continue
		}
		kept = append(kept, lines[index])
	}
	return strings.TrimSpace(strings.Join(kept, "\n")), imports
}

type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	remaining := maxEvalOutputBytes - b.Len()
	if remaining <= 0 {
		return len(p), nil
	}
	if len(p) > remaining {
		_, _ = b.Buffer.Write(p[:remaining])
		return len(p), nil
	}
	return b.Buffer.Write(p)
}
