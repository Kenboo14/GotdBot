package utils

import (
	"fmt"
	"log"
	"strings"
)

func LogError(msg string, err error, fields map[string]interface{}) {
	if err == nil {
		return
	}

	// Format: [ERROR] Pesan | error=... | field1=val1 field2=val2
	var fieldsStr strings.Builder
	for k, v := range fields {
		fieldsStr.WriteString(fmt.Sprintf("%s=%v ", k, v))
	}

	log.Printf("❌ [ERROR] %s | err=%v | %s", msg, err, fieldsStr.String())
}
