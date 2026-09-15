package utils

import (
	"fmt"
)

func Error(msg string, args ...interface{}) error {
	return fmt.Errorf(msg, args...)
}
