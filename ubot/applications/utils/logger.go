package utils

import (
	"log"
)

func Info(msg string, v ...interface{}) {
	log.Println("[INFO]", msg, v)
}

func Error(msg string, v ...interface{}) {
	log.Println("[ERROR]", msg, v)
}

func Warn(msg string, v ...interface{}) {
	log.Println("[WARN]", msg, v)
}
