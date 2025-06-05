package internal

import (
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"log"
	"runtime"
	"strings"
)

// GenerateDBName generates a unique database name based on test function and test name
func GenerateDBName(testPath string) string {
	hash := sha512.Sum512([]byte(testPath))

	encoded := base64.URLEncoding.EncodeToString(hash[:42])
	dbName := fmt.Sprintf("_sqlpt_%s", encoded)
	dbName = strings.ReplaceAll(dbName, "-", "_")

	if len(dbName) != 63 {
		log.Fatalf("generated database name '%s' is not 63 characters long, got %d", dbName, len(dbName))
	}

	return dbName
}

// GetTestFuncName returns the name of the test function that is currently running, including the package path
func GetTestFuncName() string {
	var prevFuncName string

	for i := 1; ; i++ {
		pc, _, _, ok := runtime.Caller(i)
		if !ok {
			break
		}

		funcName := runtime.FuncForPC(pc).Name()
		if funcName == "testing.tRunner" {
			break
		}
		prevFuncName = funcName
	}

	return prevFuncName
}
