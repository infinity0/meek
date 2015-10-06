// +build windows

package main

import (
	"errors"
	"bufio"
	"log"
	"os"
	"strings"
)

const browserHelperToCmdLineHelp = "path to descriptor file for browser helper executable"

// Convert the helper filename into a command line string slice
func browserHelperToCmdLine(browserHelperPath string) (command []string, err error) {
	var file *os.File
	file, err = os.Open(browserHelperPath)
	if err != nil {
		return
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	settingEnv := true
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		} else if settingEnv && strings.Contains(line, "=") {
			envpair := strings.SplitN(line, "=", 2)
			if envpair[1] == "" {
				log.Printf("unset envvar %s", envpair[0])
				err = os.Unsetenv(envpair[0])
			} else {
				log.Printf("set envvar %s=%s", envpair[0], envpair[1])
				err = os.Setenv(envpair[0], envpair[1])
			}
			if err != nil {
				return
			}
		} else {
			settingEnv = false
			command = append(command, line)
		}
	}
	err = scanner.Err()
	if err != nil {
		return
	}
	if (len(command) == 0) {
		err = errors.New("no commands in meek-browser-helper file: " + browserHelperPath)
		return
	}
	return
}
