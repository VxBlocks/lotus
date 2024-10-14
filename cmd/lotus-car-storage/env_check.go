package main

import (
	"fmt"
	"os"
)

func showEnv() {
	log.Infof("TMPDIR: %s", checkEnv("TMPDIR", false))
	log.Infof("CAR_STORAGE_PATHS: %s", checkEnv("CAR_STORAGE_PATHS", false))

}

func SetEnv(k, v string) {
	os.Setenv(k, v)
}

func unSetEnv() {
	os.Unsetenv("CAR_STORAGE_PATHS")
}
func checkEnv(env string, must bool) string {
	e := os.Getenv(env)
	if e == "" {
		if must {
			panic(fmt.Sprintf("env: %s can't empty", env))
		}
	}
	return e
}
